// Package retrieval calls the embedder + Qdrant to fetch top-K related notes.
package retrieval

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

type Client struct {
	EmbedBase  string
	EmbedModel string
	QdrantURL  string
	Collection string
	hc         *http.Client
}

func New(embedBase, embedModel, qdrantURL, collection string) *Client {
	return &Client{
		EmbedBase:  strings.TrimRight(embedBase, "/"),
		EmbedModel: embedModel,
		QdrantURL:  strings.TrimRight(qdrantURL, "/"),
		Collection: collection,
		hc:         &http.Client{Timeout: 60 * time.Second},
	}
}

type Hit struct {
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Tags     []string `json:"tags"`
	Category string   `json:"category"`
	Score    float64  `json:"score"`
}

func (c *Client) Search(ctx context.Context, queryText string, k int) ([]Hit, error) {
	vec, err := c.embed(ctx, queryText)
	if err != nil {
		return nil, err
	}

	body, _ := json.Marshal(map[string]any{
		"vector":       vec,
		"limit":        k,
		"with_payload": true,
	})
	u := fmt.Sprintf("%s/collections/%s/points/search", c.QdrantURL, url.PathEscape(c.Collection))
	r, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("qdrant: %s: %s", resp.Status, string(raw))
	}
	var qr struct {
		Result []struct {
			Score   float64                `json:"score"`
			Payload map[string]interface{} `json:"payload"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return nil, err
	}

	// Dedupe by path: each file produces multiple chunks. Keep best score per path.
	type best struct {
		hit   Hit
		score float64
	}
	dedup := map[string]best{}
	for _, r := range qr.Result {
		path, _ := r.Payload["path"].(string)
		if path == "" {
			continue
		}
		title, _ := r.Payload["title"].(string)
		category, _ := r.Payload["category"].(string)
		tags := stringList(r.Payload["tags"])
		cur, ok := dedup[path]
		if ok && cur.score >= r.Score {
			continue
		}
		dedup[path] = best{
			hit: Hit{
				Path:     path,
				Title:    title,
				Tags:     tags,
				Category: category,
				Score:    r.Score,
			},
			score: r.Score,
		}
	}
	out := make([]Hit, 0, len(dedup))
	for _, b := range dedup {
		out = append(out, b.hit)
	}
	return out, nil
}

func stringList(v interface{}) []string {
	xs, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (c *Client) embed(ctx context.Context, text string) ([]float64, error) {
	body, _ := json.Marshal(map[string]any{
		"model": c.EmbedModel,
		"input": []string{text},
	})
	r, err := http.NewRequestWithContext(ctx, "POST", c.EmbedBase+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed: %s: %s", resp.Status, string(raw))
	}
	var er struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, err
	}
	if len(er.Data) == 0 {
		return nil, fmt.Errorf("embed: empty response")
	}
	return er.Data[0].Embedding, nil
}
