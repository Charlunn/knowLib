// Package config wires env vars into a single struct for the tidy worker.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Listen string

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

	TopK      int
	MaxTokens int

	// Path to the user-editable prompt template inside the vault. Tidy reads
	// this on every run so the user can iterate without restarting the worker.
	PromptPath string
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}
func envInt(k string, def int) int {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func Load() (*Config, error) {
	c := &Config{
		Listen:           envOr("LISTEN_ADDR", ":8081"),
		VaultPath:        envOr("VAULT_PATH", "/data/vault"),
		InboxDir:         envOr("INBOX_DIR", "inbox"),
		NotesDir:         envOr("NOTES_DIR", "notes"),
		AtlasDir:         envOr("ATLAS_DIR", "atlas"),
		KnowlibDir:       envOr("KNOWLIB_DIR", ".knowlib"),
		OpenAIBaseURL:    envOr("OPENAI_BASE_URL", "https://api.deepseek.com/v1"),
		OpenAIAPIKey:     envOr("OPENAI_API_KEY", ""),
		OpenAIModel:      envOr("OPENAI_MODEL", "deepseek-chat"),
		EmbedBaseURL:     envOr("EMBED_BASE_URL", "http://embedder:8000/v1"),
		EmbedModel:       envOr("EMBED_MODEL", "bge-m3"),
		QdrantURL:        envOr("QDRANT_URL", "http://qdrant:6333"),
		QdrantCollection: envOr("QDRANT_COLLECTION", "knowlib_notes"),
		TopK:             envInt("TIDY_TOP_K", 8),
		MaxTokens:        envInt("TIDY_MAX_TOKENS", 4096),
	}
	c.PromptPath = c.VaultPath + "/" + c.KnowlibDir + "/prompts/tidy.md"
	if c.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}
	return c, nil
}
