// Package handlers wires HTTP routes to vault + store + downstream services.
package handlers

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charlunn/knowlib/api/internal/auth"
	"github.com/charlunn/knowlib/api/internal/config"
	"github.com/charlunn/knowlib/api/internal/httpx"
	"github.com/charlunn/knowlib/api/internal/store"
	"github.com/charlunn/knowlib/api/internal/vault"
	"golang.org/x/time/rate"
)

type Deps struct {
	Cfg    *config.Config
	Vault  *vault.Vault
	Store  *store.Store
	Embed  *httpx.EmbedClient
	Qdrant *httpx.QdrantClient
	Tidy   *httpx.TidyClient
}

// === helpers ==================================================================

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// === auth: TOTP login + rate limit ===========================================

type loginLimiter struct {
	mu    sync.Mutex
	limit rate.Limit
	burst int
	bag   map[string]*rate.Limiter
}

func newLoginLimiter(perMinute int) *loginLimiter {
	return &loginLimiter{
		limit: rate.Every(time.Minute / time.Duration(max(perMinute, 1))),
		burst: max(perMinute, 1),
		bag:   make(map[string]*rate.Limiter),
	}
}

func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.bag[ip]
	if !ok {
		r = rate.NewLimiter(l.limit, l.burst)
		l.bag[ip] = r
	}
	return r.Allow()
}

func clientIP(r *http.Request) string {
	// Caddy sets X-Forwarded-For; trust it because we're not exposed directly.
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		return strings.TrimSpace(strings.Split(h, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func Login(d *Deps) http.HandlerFunc {
	limiter := newLoginLimiter(d.Cfg.LoginRatePerMinute)
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.allow(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "too many login attempts; try again later")
			return
		}
		var in struct {
			TOTP string `json:"totp"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "bad json")
			return
		}
		if !auth.VerifyTOTP(d.Cfg.TOTPSecret, in.TOTP, time.Now()) {
			writeError(w, http.StatusUnauthorized, "invalid totp")
			return
		}
		tok, exp, err := auth.IssueSession(d.Cfg.JWTSecret, d.Cfg.TOTPAcct)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// HttpOnly cookie for browser; also returned in body so non-browser clients can use.
		http.SetCookie(w, &http.Cookie{
			Name:     "klib_session",
			Value:    tok,
			Path:     "/",
			Expires:  exp,
			HttpOnly: true,
			Secure:   true, // Caddy fronts us with HTTPS
			SameSite: http.SameSiteLaxMode,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"token":      tok,
			"expires_at": exp.Format(time.RFC3339),
		})
	}
}

// === capture ==================================================================

func Capture(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Content string `json:"content"`
			Source  string `json:"source"`
			TS      string `json:"ts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "bad json")
			return
		}
		content := strings.TrimRight(in.Content, " \t\n\r")
		if content == "" {
			writeError(w, http.StatusBadRequest, "empty content")
			return
		}
		ts := time.Now()
		if in.TS != "" {
			if t, err := time.Parse(time.RFC3339, in.TS); err == nil {
				ts = t
			}
		}
		// inbox/<RFC3339-Z>-<8hex>.md, slashes safe in filename
		stamp := ts.UTC().Format("20060102T150405Z")
		// Random-ish hash to avoid collisions when two devices write in the same second.
		h := sha1.Sum([]byte(content + stamp + randomNonce()))
		hash := hex.EncodeToString(h[:])[:8]
		rel := path.Join(d.Cfg.InboxDir, fmt.Sprintf("%s-%s.md", stamp, hash))

		body := buildInboxBody(content, in.Source, ts)
		if err := d.Vault.WriteAtomic(rel, []byte(body)); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"path": rel})
	}
}

func randomNonce() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func buildInboxBody(content, source string, ts time.Time) string {
	if source == "" {
		source = "api"
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("captured_at: %s\n", ts.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("source: %s\n", source))
	b.WriteString("---\n\n")
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// === inbox + list + note ======================================================

func Inbox(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Include vault root .md files because Obsidian creates new notes
		// at the vault root by default (clicking an unresolved wikilink).
		out, err := d.Vault.ListInboxAndRoot()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func List(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		category := strings.Trim(r.URL.Query().Get("category"), "/")
		base := d.Cfg.NotesDir
		if category != "" {
			base = path.Join(d.Cfg.NotesDir, category)
		}
		out, err := d.Vault.ListSubtree(base)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func Note(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		if p == "" {
			writeError(w, http.StatusBadRequest, "path is required")
			return
		}
		body, err := d.Vault.Read(p)
		if err != nil {
			if errors.Is(err, vault.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		fm, content := splitFrontmatter(string(body))
		writeJSON(w, http.StatusOK, map[string]any{
			"path":        p,
			"frontmatter": fm,
			"body":        content,
			"title":       deriveTitle(fm, content, p),
		})
	}
}

func splitFrontmatter(s string) (map[string]any, string) {
	// Minimal YAML frontmatter splitter: returns raw key/value pairs + body.
	// We do NOT pull in a full YAML lib because the api never writes back.
	if !strings.HasPrefix(s, "---\n") {
		return map[string]any{}, s
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		// also allow `---\r\n`
		end = strings.Index(rest, "\n---\r\n")
		if end < 0 {
			return map[string]any{}, s
		}
	}
	header := rest[:end]
	body := strings.TrimLeft(rest[end:], "\n-\r")
	body = strings.TrimPrefix(body, "\n")
	fm := map[string]any{}
	for _, line := range strings.Split(header, "\n") {
		i := strings.Index(line, ":")
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		v = strings.Trim(v, "\"'")
		fm[k] = v
	}
	return fm, body
}

func deriveTitle(fm map[string]any, body, p string) string {
	if t, ok := fm["title"].(string); ok && t != "" {
		return t
	}
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "# ") {
			return strings.TrimSpace(strings.TrimLeft(s, "# "))
		}
		if s != "" {
			if len(s) > 60 {
				return s[:60]
			}
			return s
		}
	}
	return strings.TrimSuffix(path.Base(p), ".md")
}

// === search ===================================================================

func Search(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if strings.TrimSpace(q) == "" {
			writeError(w, http.StatusBadRequest, "q is required")
			return
		}
		k := 10
		if v := r.URL.Query().Get("k"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
				k = n
			}
		}
		ctx := r.Context()
		vec, err := d.Embed.Embed(ctx, q)
		if err != nil {
			writeError(w, http.StatusBadGateway, "embed: "+err.Error())
			return
		}
		hits, err := d.Qdrant.Search(ctx, vec, k)
		if err != nil {
			writeError(w, http.StatusBadGateway, "qdrant: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, hits)
	}
}

// === tidy =====================================================================

func Tidy(d *Deps) http.HandlerFunc {
	defaults := buildDefaultSettings(d.Cfg)
	return func(w http.ResponseWriter, r *http.Request) {
		var in httpx.TidyRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "bad json")
			return
		}
		// Sanity-check paths so a buggy client can't ask the tidy worker to chew on /etc/passwd.
		for _, p := range in.Paths {
			if _, err := d.Vault.ResolvePath(p); err != nil {
				writeError(w, http.StatusBadRequest, "bad path: "+p)
				return
			}
		}
		// Inject current settings as overrides so user changes from /api/settings
		// take effect without restarting the tidy worker.
		st, err := d.Store.GetSettings(defaults)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Caller-provided overrides win; otherwise we fill in from the bbolt store.
		if in.Overrides == nil {
			in.Overrides = &httpx.TidyOverrides{}
		}
		if in.Overrides.LLMBaseURL == "" {
			in.Overrides.LLMBaseURL = st.LLMBaseURL
		}
		if in.Overrides.LLMAPIKey == "" {
			in.Overrides.LLMAPIKey = st.LLMAPIKey
		}
		if in.Overrides.LLMModel == "" {
			in.Overrides.LLMModel = st.LLMModel
		}
		if in.Overrides.EmbedBaseURL == "" {
			in.Overrides.EmbedBaseURL = st.EmbedBaseURL
		}
		if in.Overrides.EmbedModel == "" {
			in.Overrides.EmbedModel = st.EmbedModel
		}
		if in.Overrides.TopK == 0 {
			in.Overrides.TopK = st.TidyTopK
		}
		if in.Overrides.MaxTokens == 0 {
			in.Overrides.MaxTokens = st.TidyMaxTokens
		}
		if in.Overrides.PromptInline == "" {
			in.Overrides.PromptInline = st.TidyPrompt
		}
		out, err := d.Tidy.Run(r.Context(), in)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// === settings =================================================================

func GetSettings(d *Deps) http.HandlerFunc {
	defaults := buildDefaultSettings(d.Cfg)
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := d.Store.GetSettings(defaults)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, settingsResponse(st))
	}
}

func PutSettings(d *Deps) http.HandlerFunc {
	defaults := buildDefaultSettings(d.Cfg)
	return func(w http.ResponseWriter, r *http.Request) {
		// Only sessions (interactive) may change settings; long-lived API tokens
		// shouldn't have admin powers — keep blast radius small if one leaks.
		cred, _ := auth.CredFrom(r.Context())
		if cred.Kind != "session" {
			writeError(w, http.StatusForbidden, "settings require session login")
			return
		}
		var in struct {
			LLM struct {
				BaseURL string `json:"base_url"`
				Model   string `json:"model"`
				APIKey  string `json:"api_key"` // empty = leave unchanged
			} `json:"llm"`
			Embed struct {
				BaseURL string `json:"base_url"`
				Model   string `json:"model"`
			} `json:"embed"`
			Tidy struct {
				TopK      int    `json:"top_k"`
				MaxTokens int    `json:"max_tokens"`
				Mode      string `json:"mode"`
				Cron      string `json:"cron"`
				Prompt    string `json:"prompt"`
			} `json:"tidy"`
			AutoTidy struct {
				Enabled   bool   `json:"enabled"`
				Threshold int    `json:"threshold"`
				CronSpec  string `json:"cron_spec"`
			} `json:"auto_tidy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "bad json")
			return
		}
		// Tidy mode now supports manual (default), scheduled, threshold, both.
		switch in.Tidy.Mode {
		case "", "manual", "scheduled", "threshold", "both":
		default:
			writeError(w, http.StatusBadRequest, "tidy.mode: must be manual|scheduled|threshold|both")
			return
		}
		st := store.Settings{
			LLMBaseURL:        in.LLM.BaseURL,
			LLMModel:          in.LLM.Model,
			LLMAPIKey:         in.LLM.APIKey,
			EmbedBaseURL:      in.Embed.BaseURL,
			EmbedModel:        in.Embed.Model,
			TidyTopK:          in.Tidy.TopK,
			TidyMaxTokens:     in.Tidy.MaxTokens,
			TidyMode:          in.Tidy.Mode,
			TidyCron:          in.Tidy.Cron,
			TidyPrompt:        in.Tidy.Prompt,
			AutoTidyEnabled:   in.AutoTidy.Enabled,
			AutoTidyThreshold: in.AutoTidy.Threshold,
			AutoTidyCronSpec:  in.AutoTidy.CronSpec,
		}
		if err := d.Store.PutSettings(st); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		got, err := d.Store.GetSettings(defaults)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, settingsResponse(got))
	}
}

func buildDefaultSettings(cfg *config.Config) store.Settings {
	return store.Settings{
		LLMBaseURL:    cfg.OpenAIBaseURL,
		LLMModel:      cfg.OpenAIModel,
		LLMAPIKey:     cfg.OpenAIAPIKey,
		EmbedBaseURL:  cfg.EmbedBaseURL,
		EmbedModel:    cfg.EmbedModel,
		TidyTopK:      cfg.TidyTopK,
		TidyMaxTokens: cfg.TidyMaxTokens,
		TidyMode:      "manual",
		TidyPrompt:    "", // tidy worker reads from the file system if blank here
	}
}

// settingsResponse strips the api_key but exposes whether it's set.
func settingsResponse(st store.Settings) map[string]any {
	return map[string]any{
		"llm": map[string]any{
			"base_url":    st.LLMBaseURL,
			"model":       st.LLMModel,
			"api_key_set": st.LLMAPIKey != "",
		},
		"embed": map[string]any{
			"base_url": st.EmbedBaseURL,
			"model":    st.EmbedModel,
		},
		"tidy": map[string]any{
			"top_k":      st.TidyTopK,
			"max_tokens": st.TidyMaxTokens,
			"mode":       st.TidyMode,
			"cron":       st.TidyCron,
			"prompt":     st.TidyPrompt,
		},
		"auto_tidy": map[string]any{
			"enabled":   st.AutoTidyEnabled,
			"threshold": st.AutoTidyThreshold,
			"cron_spec": st.AutoTidyCronSpec,
		},
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// === logout ===================================================================

func Logout(_ *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Session JWTs are stateless; logout is just clearing the cookie.
		http.SetCookie(w, &http.Cookie{
			Name:     "klib_session",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// === test-llm =================================================================

func TestLLM(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "bad json")
			return
		}
		if strings.TrimSpace(in.Prompt) == "" {
			in.Prompt = "say ok"
		}
		// Use current settings so the test reflects whatever LLM the user has configured.
		defaults := buildDefaultSettings(d.Cfg)
		st, err := d.Store.GetSettings(defaults)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Build a one-shot HTTP client to call the LLM.
		type msg struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		type chatReq struct {
			Model     string  `json:"model"`
			Messages  []msg   `json:"messages"`
			MaxTokens int     `json:"max_tokens"`
			Temp      float32 `json:"temperature"`
		}
		type choice struct {
			Message msg `json:"message"`
		}
		type chatResp struct {
			Choices []choice `json:"choices"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}

		reqBody, _ := json.Marshal(chatReq{
			Model:     st.LLMModel,
			Messages:  []msg{{Role: "user", Content: in.Prompt}},
			MaxTokens: 256,
			Temp:      0,
		})
		apiKey := st.LLMAPIKey
		if apiKey == "" {
			apiKey = d.Cfg.OpenAIAPIKey
		}
		baseURL := strings.TrimRight(st.LLMBaseURL, "/")
		if baseURL == "" {
			baseURL = strings.TrimRight(d.Cfg.OpenAIBaseURL, "/")
		}

		hc := &http.Client{Timeout: 30 * time.Second}
		req, err := http.NewRequestWithContext(r.Context(), "POST", baseURL+"/chat/completions",
			strings.NewReader(string(reqBody)))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := hc.Do(req)
		if err != nil {
			writeError(w, http.StatusBadGateway, "llm: "+err.Error())
			return
		}
		defer resp.Body.Close()
		var cr chatResp
		if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
			writeError(w, http.StatusBadGateway, "llm parse: "+err.Error())
			return
		}
		if cr.Error.Message != "" {
			writeError(w, http.StatusBadGateway, "llm: "+cr.Error.Message)
			return
		}
		if len(cr.Choices) == 0 {
			writeError(w, http.StatusBadGateway, "llm: empty response")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": cr.Choices[0].Message.Content})
	}
}
