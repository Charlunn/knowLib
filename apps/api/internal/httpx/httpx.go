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
}

func NewEmbedClient(baseURL, model string) *EmbedClient {
	return &EmbedClient{BaseURL: strings.TrimRight(baseURL, "/"), Model: model, hc: newClient()}
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

func (c *EmbedClient) Embed(ctx context.Context, input string) ([]float64, error) {
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
	Paths []string `json:"paths,omitempty"`
	All   bool     `json:"all,omitempty"`
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
