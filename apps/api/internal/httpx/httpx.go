// Package httpx contains thin client wrappers around the OpenAI-compatible
// embedder, Qdrant, and the tidy worker. They live in api/ rather than a
// shared module because each is small and the two services that need them
// (api, tidy) want different things.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

func newClient() *http.Client {
	return &http.Client{Timeout: 60 * time.Second}
}

// === Embedder client ==========================================================

type EmbedClient struct {
	BaseURL string // e.g. http://embedder:8000/v1
	Model   string
	hc      *http.Client

	// Small in-memory LRU cache so repeated identical search queries (or
	// tidy retrievals on the same body) don't re-embed. Embedding is the
	// dominant per-request cost; even a tiny cache pays for itself fast.
	cacheMu sync.Mutex
	cache   map[string]embedCacheEntry
	cacheLRU []string // path-of-keys, oldest-first
}

const embedCacheMax = 128
const embedCacheTTL = 10 * time.Minute

type embedCacheEntry struct {
	vec  []float64
	when time.Time
}

func NewEmbedClient(baseURL, model string) *EmbedClient {
	return &EmbedClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		hc:      newClient(),
		cache:   make(map[string]embedCacheEntry, embedCacheMax),
	}
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}
type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (c *EmbedClient) cacheGet(key string) ([]float64, bool) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	e, ok := c.cache[key]
	if !ok {
		return nil, false
	}
	if time.Since(e.when) > embedCacheTTL {
		delete(c.cache, key)
		return nil, false
	}
	return e.vec, true
}

func (c *EmbedClient) cachePut(key string, vec []float64) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if _, exists := c.cache[key]; !exists {
		c.cacheLRU = append(c.cacheLRU, key)
		// Evict oldest entries when we go over the cap.
		for len(c.cacheLRU) > embedCacheMax {
			oldest := c.cacheLRU[0]
			c.cacheLRU = c.cacheLRU[1:]
			delete(c.cache, oldest)
		}
	}
	c.cache[key] = embedCacheEntry{vec: vec, when: time.Now()}
}

func (c *EmbedClient) Embed(ctx context.Context, input string) ([]float64, error) {
	cacheKey := c.Model + "\x00" + input
	if vec, ok := c.cacheGet(cacheKey); ok {
		return vec, nil
	}
	body, _ := json.Marshal(embedRequest{Model: c.Model, Input: []string{input}})
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed: %s: %s", resp.Status, string(b))
	}
	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, err
	}
	if len(er.Data) == 0 {
		return nil, fmt.Errorf("embed: empty response")
	}
	c.cachePut(cacheKey, er.Data[0].Embedding)
	return er.Data[0].Embedding, nil
}

// === Qdrant client ============================================================

type QdrantClient struct {
	BaseURL    string
	Collection string
	hc         *http.Client
}

func NewQdrantClient(baseURL, collection string) *QdrantClient {
	return &QdrantClient{BaseURL: strings.TrimRight(baseURL, "/"), Collection: collection, hc: newClient()}
}

type SearchHit struct {
	ID      string                 `json:"id"`
	Score   float64                `json:"score"`
	Path    string                 `json:"path"`
	Title   string                 `json:"title"`
	Snippet string                 `json:"snippet"`
	Payload map[string]interface{} `json:"payload"`
}

type qdrantSearchReq struct {
	Vector      []float64 `json:"vector"`
	Limit       int       `json:"limit"`
	WithPayload bool      `json:"with_payload"`
}
type qdrantSearchResp struct {
	Result []struct {
		ID      json.RawMessage        `json:"id"`
		Score   float64                `json:"score"`
		Payload map[string]interface{} `json:"payload"`
	} `json:"result"`
}

func (c *QdrantClient) Search(ctx context.Context, vec []float64, k int) ([]SearchHit, error) {
	if k <= 0 {
		k = 10
	}
	body, _ := json.Marshal(qdrantSearchReq{Vector: vec, Limit: k, WithPayload: true})
	u := fmt.Sprintf("%s/collections/%s/points/search", c.BaseURL, url.PathEscape(c.Collection))
	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("qdrant search: %s: %s", resp.Status, string(b))
	}
	var qr qdrantSearchResp
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return nil, err
	}
	out := make([]SearchHit, 0, len(qr.Result))
	for _, r := range qr.Result {
		path, _ := r.Payload["path"].(string)
		title, _ := r.Payload["title"].(string)
		content, _ := r.Payload["content"].(string)
		// snippet: first 200 chars of content
		snip := content
		if len(snip) > 200 {
			snip = snip[:200]
		}
		out = append(out, SearchHit{
			ID:      string(r.ID),
			Score:   r.Score,
			Path:    path,
			Title:   title,
			Snippet: snip,
			Payload: r.Payload,
		})
	}
	return out, nil
}

// === Tidy client ==============================================================

type TidyClient struct {
	BaseURL string
	hc      *http.Client
}

func NewTidyClient(baseURL string) *TidyClient {
	return &TidyClient{BaseURL: strings.TrimRight(baseURL, "/"), hc: newClient()}
}

type TidyRequest struct {
	Paths     []string       `json:"paths,omitempty"`
	All       bool           `json:"all,omitempty"`
	Overrides *TidyOverrides `json:"overrides,omitempty"`
}

// TidyOverrides carries the user's current LLM/Embed/Tidy settings from the
// api's bbolt store to the tidy worker. Without this the tidy worker would
// only ever see the env vars from docker-compose, defeating the Web settings
// page.
type TidyOverrides struct {
	LLMBaseURL    string `json:"llm_base_url,omitempty"`
	LLMAPIKey     string `json:"llm_api_key,omitempty"`
	LLMModel      string `json:"llm_model,omitempty"`
	EmbedBaseURL  string `json:"embed_base_url,omitempty"`
	EmbedModel    string `json:"embed_model,omitempty"`
	TopK          int    `json:"top_k,omitempty"`
	MaxTokens     int    `json:"max_tokens,omitempty"`
	PromptInline  string `json:"prompt_inline,omitempty"`
}
type TidyItem struct {
	SourcePath string `json:"source_path"`
	TargetPath string `json:"target_path,omitempty"`
	Status     string `json:"status"` // ok | partial | failed
	Reason     string `json:"reason,omitempty"`
}
type TidyResponse struct {
	JobID string     `json:"job_id"`
	Items []TidyItem `json:"items"`
}

func (c *TidyClient) Run(ctx context.Context, req TidyRequest) (*TidyResponse, error) {
	body, _ := json.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/run", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	// Tidy can take a while if the user picks "all"; allow a long timeout.
	hc := &http.Client{Timeout: 10 * time.Minute}
	resp, err := hc.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tidy: %s: %s", resp.Status, string(b))
	}
	var tr TidyResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	return &tr, nil
}

// === AI ops client ============================================================

// AIOpsRequest is forwarded to /ai-action.
type AIOpsRequest struct {
	Action      string             `json:"action"`
	Paths       []string           `json:"paths,omitempty"`
	Folder      string             `json:"folder,omitempty"`
	PreviewOnly bool               `json:"preview_only"`
	Overrides   *AIOpsOverrides    `json:"overrides,omitempty"`
}

type AIOpsOverrides struct {
	LLMBaseURL   string `json:"llm_base_url,omitempty"`
	LLMAPIKey    string `json:"llm_api_key,omitempty"`
	LLMModel     string `json:"llm_model,omitempty"`
	MaxTokens    int    `json:"max_tokens,omitempty"`
	PromptInline string `json:"prompt_inline,omitempty"`
}

// AIOpsOperation matches aiops.Operation in the tidy worker.
type AIOpsOperation struct {
	Type    string `json:"type"`
	Path    string `json:"path,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Content string `json:"content,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Before  string `json:"before,omitempty"`
	Summary string `json:"summary,omitempty"`
	Issues  []any  `json:"issues,omitempty"`
}

type AIOpsResponse struct {
	Action     string           `json:"action"`
	Summary    string           `json:"summary"`
	Operations []AIOpsOperation `json:"operations"`
}

func (c *TidyClient) RunAIOps(ctx context.Context, req AIOpsRequest) (*AIOpsResponse, error) {
	body, _ := json.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/ai-action", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	hc := &http.Client{Timeout: 10 * time.Minute}
	resp, err := hc.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ai-action: %s: %s", resp.Status, string(b))
	}
	var out AIOpsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *TidyClient) ApplyAIOps(ctx context.Context, resp AIOpsResponse) error {
	body, _ := json.Marshal(resp)
	r, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/ai-action/apply", bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	hc := &http.Client{Timeout: 5 * time.Minute}
	httpResp, err := hc.Do(r)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != 200 {
		b, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("apply: %s: %s", httpResp.Status, string(b))
	}
	return nil
}
