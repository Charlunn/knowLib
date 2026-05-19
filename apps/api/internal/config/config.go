// Package config wires the env vars from docker-compose.yml + .env into a
// single Go struct. Defaults match `.env.example` so local-dev `go run ./cmd/api`
// works without docker.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Domain     string
	Listen     string // host:port the server binds to
	JWTSecret  string
	TOTPSecret string // base32
	TOTPIssuer string
	TOTPAcct   string

	VaultPath  string
	InboxDir   string
	NotesDir   string
	AtlasDir   string
	KnowlibDir string

	OpenAIBaseURL string
	OpenAIAPIKey  string
	OpenAIModel   string

	EmbedBaseURL string
	EmbedModel   string

	QdrantURL        string
	QdrantCollection string

	TidyServiceURL string
	TidyTopK       int
	TidyMaxTokens  int

	StatePath          string // bbolt db location
	SkillBundleDir     string // packages/skill-bundle (read-only template)
	LoginRatePerMinute int
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func Load() (*Config, error) {
	c := &Config{
		Domain:             envOr("DOMAIN", "localhost"),
		Listen:             envOr("LISTEN_ADDR", ":8080"),
		JWTSecret:          envOr("JWT_SECRET", ""),
		TOTPSecret:         envOr("TOTP_SECRET", ""),
		TOTPIssuer:         envOr("TOTP_ISSUER", "knowLib"),
		TOTPAcct:           envOr("TOTP_ACCOUNT", "me"),
		VaultPath:          envOr("VAULT_PATH", "/data/vault"),
		InboxDir:           envOr("INBOX_DIR", "inbox"),
		NotesDir:           envOr("NOTES_DIR", "notes"),
		AtlasDir:           envOr("ATLAS_DIR", "atlas"),
		KnowlibDir:         envOr("KNOWLIB_DIR", ".knowlib"),
		OpenAIBaseURL:      envOr("OPENAI_BASE_URL", "https://api.deepseek.com/v1"),
		OpenAIAPIKey:       envOr("OPENAI_API_KEY", ""),
		OpenAIModel:        envOr("OPENAI_MODEL", "deepseek-chat"),
		EmbedBaseURL:       envOr("EMBED_BASE_URL", "http://embedder:8000/v1"),
		EmbedModel:         envOr("EMBED_MODEL", "bge-m3"),
		QdrantURL:          envOr("QDRANT_URL", "http://qdrant:6333"),
		QdrantCollection:   envOr("QDRANT_COLLECTION", "knowlib_notes"),
		TidyServiceURL:     envOr("TIDY_SERVICE_URL", "http://tidy:8081"),
		TidyTopK:           envInt("TIDY_TOP_K", 8),
		TidyMaxTokens:      envInt("TIDY_MAX_TOKENS", 4096),
		StatePath:          envOr("API_STATE_PATH", "/state/api.db"),
		SkillBundleDir:     envOr("SKILL_BUNDLE_DIR", "/skill-bundle"),
		LoginRatePerMinute: envInt("LOGIN_RATE_PER_MINUTE", 5),
	}
	if c.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	if c.TOTPSecret == "" {
		return nil, fmt.Errorf("TOTP_SECRET is required (run scripts/bootstrap.sh first)")
	}
	return c, nil
}
