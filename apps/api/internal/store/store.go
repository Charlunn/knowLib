// Package store persists settings, API tokens, and login rate-limit state in
// a single bbolt file ($API_STATE_PATH). bbolt is appropriate here because:
//   - We're a single-writer service.
//   - Volume is tiny (a few KB).
//   - No external DB to operate.
package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketSettings = []byte("settings")
	bucketTokens   = []byte("tokens")    // id -> tokenRecord JSON
	bucketTokenIDX = []byte("tokens_ix") // sha -> id
)

type Store struct {
	db   *bolt.DB
	gcm  cipher.AEAD // for encrypting LLM api keys at rest
	once sync.Once
}

// Open creates / opens the bbolt file. encKey is derived from JWT_SECRET so
// secrets at rest are protected by the same blast radius as session tokens.
func Open(path string, encKey []byte) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketSettings, bucketTokens, bucketTokenIDX} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, err
	}

	sum := sha256.Sum256(encKey)
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, gcm: g}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// === settings =================================================================

// Settings is the JSON shape persisted to bbolt and exposed on /api/settings.
// The api_key is stored encrypted; SetAPIKey sets it, GetAPIKeyClear decrypts.
type Settings struct {
	LLMBaseURL string `json:"llm_base_url"`
	LLMModel   string `json:"llm_model"`
	LLMAPIKey  string `json:"-"` // never serialized; replaced by api_key_set in API response

	EmbedBaseURL string `json:"embed_base_url"`
	EmbedModel   string `json:"embed_model"`

	TidyTopK      int    `json:"tidy_top_k"`
	TidyMaxTokens int    `json:"tidy_max_tokens"`
	TidyMode      string `json:"tidy_mode"` // "manual" only in Phase 1
	TidyCron      string `json:"tidy_cron,omitempty"`
	TidyPrompt    string `json:"tidy_prompt"`
}

const settingsKey = "current"

func (s *Store) GetSettings(defaults Settings) (Settings, error) {
	var st Settings
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketSettings).Get([]byte(settingsKey))
		if raw == nil {
			st = defaults
			return nil
		}
		return json.Unmarshal(raw, &st)
	})
	if err != nil {
		return Settings{}, err
	}
	// Pull encrypted API key separately.
	var clear string
	err = s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketSettings).Get([]byte("llm_api_key_enc"))
		if raw == nil {
			return nil
		}
		dec, derr := s.decrypt(raw)
		if derr != nil {
			return derr
		}
		clear = string(dec)
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	st.LLMAPIKey = clear
	if st.LLMBaseURL == "" {
		st.LLMBaseURL = defaults.LLMBaseURL
	}
	if st.LLMModel == "" {
		st.LLMModel = defaults.LLMModel
	}
	if st.EmbedBaseURL == "" {
		st.EmbedBaseURL = defaults.EmbedBaseURL
	}
	if st.EmbedModel == "" {
		st.EmbedModel = defaults.EmbedModel
	}
	if st.TidyTopK == 0 {
		st.TidyTopK = defaults.TidyTopK
	}
	if st.TidyMaxTokens == 0 {
		st.TidyMaxTokens = defaults.TidyMaxTokens
	}
	if st.TidyPrompt == "" {
		st.TidyPrompt = defaults.TidyPrompt
	}
	if st.TidyMode == "" {
		st.TidyMode = "manual"
	}
	return st, nil
}

func (s *Store) PutSettings(st Settings) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		bk := tx.Bucket(bucketSettings)
		// We persist non-secret fields as JSON.
		body := struct {
			LLMBaseURL    string `json:"llm_base_url"`
			LLMModel      string `json:"llm_model"`
			EmbedBaseURL  string `json:"embed_base_url"`
			EmbedModel    string `json:"embed_model"`
			TidyTopK      int    `json:"tidy_top_k"`
			TidyMaxTokens int    `json:"tidy_max_tokens"`
			TidyMode      string `json:"tidy_mode"`
			TidyCron      string `json:"tidy_cron,omitempty"`
			TidyPrompt    string `json:"tidy_prompt"`
		}{
			st.LLMBaseURL, st.LLMModel,
			st.EmbedBaseURL, st.EmbedModel,
			st.TidyTopK, st.TidyMaxTokens,
			st.TidyMode, st.TidyCron, st.TidyPrompt,
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		if err := bk.Put([]byte(settingsKey), raw); err != nil {
			return err
		}
		// Only update encrypted api key if a non-empty value was provided.
		if st.LLMAPIKey != "" {
			enc, err := s.encrypt([]byte(st.LLMAPIKey))
			if err != nil {
				return err
			}
			return bk.Put([]byte("llm_api_key_enc"), enc)
		}
		return nil
	})
}

// === tokens ===================================================================

type TokenRecord struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	SHA        string     `json:"sha"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

func (s *Store) CreateToken(name, sha string) (TokenRecord, error) {
	id := newRandomID()
	rec := TokenRecord{
		ID:        id,
		Name:      name,
		SHA:       sha,
		CreatedAt: time.Now(),
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		raw, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		if err := tx.Bucket(bucketTokens).Put([]byte(id), raw); err != nil {
			return err
		}
		return tx.Bucket(bucketTokenIDX).Put([]byte(sha), []byte(id))
	})
	return rec, err
}

func (s *Store) ListTokens() ([]TokenRecord, error) {
	var out []TokenRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketTokens).ForEach(func(_, v []byte) error {
			var rec TokenRecord
			if err := json.Unmarshal(v, &rec); err != nil {
				return err
			}
			out = append(out, rec)
			return nil
		})
	})
	return out, err
}

func (s *Store) RevokeToken(id string) error {
	now := time.Now()
	return s.db.Update(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketTokens).Get([]byte(id))
		if raw == nil {
			return errors.New("token not found")
		}
		var rec TokenRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return err
		}
		rec.RevokedAt = &now
		updated, _ := json.Marshal(rec)
		return tx.Bucket(bucketTokens).Put([]byte(id), updated)
	})
}

// FindBySHA implements auth.TokenLookup. Records last_used_at lazily.
func (s *Store) FindBySHA(sha string) (id string, revoked bool, ok bool) {
	_ = s.db.View(func(tx *bolt.Tx) error {
		idBytes := tx.Bucket(bucketTokenIDX).Get([]byte(sha))
		if idBytes == nil {
			return nil
		}
		id = string(idBytes)
		recRaw := tx.Bucket(bucketTokens).Get(idBytes)
		if recRaw == nil {
			return nil
		}
		var rec TokenRecord
		if err := json.Unmarshal(recRaw, &rec); err != nil {
			return err
		}
		ok = true
		revoked = rec.RevokedAt != nil
		return nil
	})
	if ok && !revoked {
		// Best-effort last_used_at update; non-blocking on failure.
		go s.touchToken(id)
	}
	return id, revoked, ok
}

func (s *Store) touchToken(id string) {
	now := time.Now()
	_ = s.db.Update(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketTokens).Get([]byte(id))
		if raw == nil {
			return nil
		}
		var rec TokenRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return err
		}
		rec.LastUsedAt = &now
		updated, _ := json.Marshal(rec)
		return tx.Bucket(bucketTokens).Put([]byte(id), updated)
	})
}

// === helpers ==================================================================

func (s *Store) encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.gcm.Seal(nonce, nonce, plain, nil), nil
}

func (s *Store) decrypt(ct []byte) ([]byte, error) {
	if len(ct) < s.gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, body := ct[:s.gcm.NonceSize()], ct[s.gcm.NonceSize():]
	return s.gcm.Open(nil, nonce, body, nil)
}

func newRandomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
