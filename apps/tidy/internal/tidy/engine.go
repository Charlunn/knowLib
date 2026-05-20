// Package tidy is the orchestrator that turns inbox files into structured
// knowledge base entries. The v2 design supports three actions:
//   - append: add the new content as a new section to an existing note
//   - create: create a new sub-note under an existing topic directory
//   - create_topic: create a new topic entry file plus a sub-note
//
// The LLM sees the existing notes/ tree before deciding, so it can route
// related content into the same topic instead of fragmenting the vault.
package tidy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charlunn/knowlib/tidy/internal/config"
	"github.com/charlunn/knowlib/tidy/internal/fmatter"
	"github.com/charlunn/knowlib/tidy/internal/llm"
	"github.com/charlunn/knowlib/tidy/internal/retrieval"
	"github.com/charlunn/knowlib/tidy/internal/vaultfs"
)

type Item struct {
	SourcePath string `json:"source_path"`
	TargetPath string `json:"target_path,omitempty"`
	Action     string `json:"action,omitempty"`
	Status     string `json:"status"` // ok | partial | failed
	Reason     string `json:"reason,omitempty"`
}

type Result struct {
	JobID string `json:"job_id"`
	Items []Item `json:"items"`
}

// Engine wires the orchestrator's dependencies together.
type Engine struct {
	Cfg     *config.Config
	FS      *vaultfs.FS
	Retr    *retrieval.Client
	LLM     *llm.Client
	NowFunc func() time.Time
}

// Overrides carry per-request settings that override env-loaded config.
// Sent by the api service so user changes via /api/settings take effect
// without restarting the tidy worker.
type Overrides struct {
	LLMBaseURL   string `json:"llm_base_url,omitempty"`
	LLMAPIKey    string `json:"llm_api_key,omitempty"`
	LLMModel     string `json:"llm_model,omitempty"`
	EmbedBaseURL string `json:"embed_base_url,omitempty"`
	EmbedModel   string `json:"embed_model,omitempty"`
	TopK         int    `json:"top_k,omitempty"`
	MaxTokens    int    `json:"max_tokens,omitempty"`
	PromptInline string `json:"prompt_inline,omitempty"`
}

func NewEngine(cfg *config.Config) *Engine {
	return &Engine{
		Cfg:     cfg,
		FS:      vaultfs.New(cfg.VaultPath, cfg.InboxDir, cfg.NotesDir, cfg.AtlasDir, cfg.KnowlibDir),
		Retr:    retrieval.New(cfg.EmbedBaseURL, cfg.EmbedModel, cfg.QdrantURL, cfg.QdrantCollection),
		LLM:     llm.New(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel),
		NowFunc: time.Now,
	}
}

// runtimeContext bundles per-request settings (LLM/embed clients can be
// rebuilt when the api forwards user-changed settings as overrides).
type runtimeContext struct {
	llm        *llm.Client
	retr       *retrieval.Client
	model      string
	topK       int
	maxTokens  int
	promptText string
}

func (e *Engine) buildRuntimeContext(o *Overrides) *runtimeContext {
	rc := &runtimeContext{
		llm:       e.LLM,
		retr:      e.Retr,
		model:     e.Cfg.OpenAIModel,
		topK:      e.Cfg.TopK,
		maxTokens: e.Cfg.MaxTokens,
	}
	if o == nil {
		return rc
	}
	if (o.LLMBaseURL != "" && o.LLMBaseURL != e.Cfg.OpenAIBaseURL) ||
		(o.LLMAPIKey != "" && o.LLMAPIKey != e.Cfg.OpenAIAPIKey) ||
		(o.LLMModel != "" && o.LLMModel != e.Cfg.OpenAIModel) {
		baseURL := pick(o.LLMBaseURL, e.Cfg.OpenAIBaseURL)
		apiKey := pick(o.LLMAPIKey, e.Cfg.OpenAIAPIKey)
		model := pick(o.LLMModel, e.Cfg.OpenAIModel)
		rc.llm = llm.New(baseURL, apiKey, model)
		rc.model = model
	} else if o.LLMModel != "" {
		rc.model = o.LLMModel
	}
	if (o.EmbedBaseURL != "" && o.EmbedBaseURL != e.Cfg.EmbedBaseURL) ||
		(o.EmbedModel != "" && o.EmbedModel != e.Cfg.EmbedModel) {
		rc.retr = retrieval.New(
			pick(o.EmbedBaseURL, e.Cfg.EmbedBaseURL),
			pick(o.EmbedModel, e.Cfg.EmbedModel),
			e.Cfg.QdrantURL,
			e.Cfg.QdrantCollection,
		)
	}
	if o.TopK > 0 {
		rc.topK = o.TopK
	}
	if o.MaxTokens > 0 {
		rc.maxTokens = o.MaxTokens
	}
	if o.PromptInline != "" {
		rc.promptText = o.PromptInline
	}
	return rc
}

func pick(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Run executes a tidy job. If `all` is true, runs over every inbox/ file.
// Items are processed with bounded concurrency so a "tidy all" over a large
// inbox doesn't take wall-clock time proportional to file count × LLM latency.
func (e *Engine) Run(ctx context.Context, paths []string, all bool, overrides *Overrides) (*Result, error) {
	if all {
		got, err := e.FS.ListInbox()
		if err != nil {
			return nil, err
		}
		paths = got
	}
	rc := e.buildRuntimeContext(overrides)
	job := &Result{JobID: fmt.Sprintf("tidy-%d", e.NowFunc().Unix())}

	if len(paths) <= 1 {
		for _, p := range paths {
			job.Items = append(job.Items, e.runOne(ctx, p, rc))
		}
		return job, nil
	}

	const maxConcurrent = 3
	sem := make(chan struct{}, maxConcurrent)
	results := make([]Item, len(paths))
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		go func(idx int, src string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = Item{SourcePath: src, Status: "failed", Reason: "cancelled"}
				return
			}
			results[idx] = e.runOne(ctx, src, rc)
		}(i, p)
	}
	wg.Wait()
	job.Items = results
	return job, nil
}

// runOne handles a single inbox file. The LLM decides the action; we execute it.
func (e *Engine) runOne(ctx context.Context, srcPath string, rc *runtimeContext) Item {
	out := Item{SourcePath: srcPath, Status: "failed"}

	raw, err := e.FS.Read(srcPath)
	if err != nil {
		out.Reason = "read: " + err.Error()
		return out
	}
	srcFM, body := fmatter.Parse(string(raw))

	// 1. Vector retrieve top-K related notes (provides semantic context).
	hits, _ := rc.retr.Search(ctx, body, rc.topK)
	relatedJSON, _ := json.MarshalIndent(simplifyHits(hits), "", "  ")

	// 2. Scan the existing notes/ tree so the LLM can route into existing
	//    topics instead of fragmenting the vault into one-file directories.
	existingTree := e.scanNotesTree()

	// 3. Load prompt: inline override > vault file > bundled default.
	prompt, err := e.loadPrompt(rc.promptText)
	if err != nil {
		out.Reason = "prompt: " + err.Error()
		return out
	}

	// 4. Ask the LLM.
	user := fmt.Sprintf(
		"=== 现有笔记目录结构 ===\n%s\n\n=== 相关笔记列表(向量检索) ===\n%s\n\n=== 原文(inbox 文件内容) ===\n%s\n",
		existingTree,
		string(relatedJSON),
		body,
	)
	rawResp, err := rc.llm.Chat(ctx, prompt, user, rc.maxTokens)
	if err != nil {
		out.Reason = "llm: " + err.Error()
		return out
	}
	parsed, err := parseLLMOutput(rawResp)
	if err != nil {
		out.Reason = "parse: " + err.Error()
		return out
	}

	// 5. Execute the action.
	now := e.NowFunc().UTC().Format(time.RFC3339)
	target := parsed.TargetPath
	if target == "" {
		target = e.composeTargetPath(parsed)
	}
	if target == "" {
		out.Reason = "no target_path or category/title in LLM output"
		return out
	}
	out.TargetPath = target
	out.Action = parsed.Action

	switch parsed.Action {
	case "append":
		if err := e.doAppend(target, parsed.Body, parsed.Title); err != nil {
			// Target missing — fall through to create.
			if errors.Is(err, vaultfs.ErrNotFound) {
				parsed.Action = "create"
			} else {
				out.Reason = "append: " + err.Error()
				return out
			}
		}
		if parsed.Action == "append" {
			break // success
		}
		fallthrough

	case "create", "create_topic", "":
		if parsed.Action == "" {
			parsed.Action = "create"
			out.Action = "create"
		}
		if err := e.doCreate(target, parsed, srcFM, now); err != nil {
			out.Reason = "create: " + err.Error()
			return out
		}
		// For create_topic, also write the index file (if not already created)
		// before applying the optional cross-reference index_update.
		if parsed.Action == "create_topic" {
			indexPath := path.Join(e.Cfg.NotesDir, safeFilename(parsed.Category)+".md")
			if !e.FS.Exists(indexPath) {
				stub := fmt.Sprintf("# %s\n\n本主题的入口文件,AI 自动维护。\n\n## 笔记列表\n\n", parsed.Category)
				_ = e.FS.WriteAtomic(indexPath, []byte(stub))
			}
		}

	default:
		out.Reason = "unknown action: " + parsed.Action
		return out
	}

	// 6. Apply the optional index_update (link from topic page to this note).
	if parsed.IndexUpdate.Path != "" && parsed.IndexUpdate.AddEntry != "" {
		e.applyIndexUpdate(parsed)
	}

	// 7. Delete the inbox source and clean up empty parent directories.
	_ = e.FS.Remove(srcPath)
	_ = e.FS.RemoveEmptyDirs(filepath.Dir(srcPath), e.FS.InboxDir)

	// 8. Log.
	_ = e.FS.AppendLog("tidy.log", fmt.Sprintf(
		"%s -> %s (action=%s, model=%s)",
		srcPath, target, parsed.Action, rc.model,
	))

	out.Status = "ok"
	return out
}

// doAppend appends content as a new section under an existing note. The new
// content is wrapped in a "## <heading>" block (or the LLM's body verbatim if
// it already starts with a heading).
func (e *Engine) doAppend(target, body, sectionTitle string) error {
	existing, err := e.FS.Read(target)
	if err != nil {
		return err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("empty append body")
	}
	// Wrap in "## title" if the body doesn't already start with a heading.
	prefix := ""
	if !strings.HasPrefix(body, "#") && sectionTitle != "" {
		prefix = "## " + sectionTitle + "\n\n"
	}
	merged := strings.TrimRight(string(existing), "\n") + "\n\n" + prefix + body + "\n"
	// Update `updated` timestamp in frontmatter if present.
	merged = bumpUpdatedTimestamp(merged, e.NowFunc().UTC().Format(time.RFC3339))
	return e.FS.WriteAtomic(target, []byte(merged))
}

// doCreate writes a new note with full frontmatter.
func (e *Engine) doCreate(target string, parsed *llmOutputV2, srcFM map[string]string, now string) error {
	keys := []string{"title", "category", "tags", "related", "created", "updated"}
	fm := map[string]any{
		"title":    parsed.Title,
		"category": parsed.Category,
		"tags":     parsed.Tags,
		"related":  parsed.Related,
		"created":  defaultStr(srcFM["captured_at"], now),
		"updated":  now,
	}
	rendered := fmatter.Render(keys, fm, parsed.Body)
	return e.FS.WriteAtomic(target, []byte(rendered))
}

// applyIndexUpdate adds a bullet entry to a topic's index file.
func (e *Engine) applyIndexUpdate(parsed *llmOutputV2) {
	indexPath := parsed.IndexUpdate.Path
	entry := strings.TrimRight(parsed.IndexUpdate.AddEntry, "\n")
	if entry == "" {
		return
	}

	existing, err := e.FS.Read(indexPath)
	if err != nil {
		// Index file doesn't exist — create it with the entry.
		content := fmt.Sprintf("# %s\n\n## 笔记列表\n\n%s\n", parsed.Category, entry)
		_ = e.FS.WriteAtomic(indexPath, []byte(content))
		return
	}
	if strings.Contains(string(existing), entry) {
		return // already present
	}
	updated := strings.TrimRight(string(existing), "\n") + "\n" + entry + "\n"
	_ = e.FS.WriteAtomic(indexPath, []byte(updated))
}

// scanNotesTree returns a compact text representation of the existing notes/
// directory structure for the LLM to reference when making classification
// decisions. Output stays small — we only emit names, not full paths.
func (e *Engine) scanNotesTree() string {
	abs, err := e.FS.Resolve(e.Cfg.NotesDir)
	if err != nil {
		return "(empty)"
	}
	var lines []string
	_ = filepath.WalkDir(abs, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel, err := filepath.Rel(abs, p)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		base := filepath.Base(rel)
		if strings.HasPrefix(base, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		depth := strings.Count(rel, "/")
		indent := strings.Repeat("  ", depth)
		if d.IsDir() {
			lines = append(lines, indent+base+"/")
		} else if strings.HasSuffix(base, ".md") {
			lines = append(lines, indent+base)
		}
		return nil
	})
	if len(lines) == 0 {
		return "(empty — 还没有笔记,你可以新建主题入口)"
	}
	return "notes/\n" + strings.Join(lines, "\n")
}

// loadPrompt: inline override > vault file > bundled default.
func (e *Engine) loadPrompt(inline string) (string, error) {
	if inline != "" {
		return inline, nil
	}
	if e.Cfg.PromptPath != "" {
		b, err := os.ReadFile(e.Cfg.PromptPath)
		if err == nil {
			return string(b), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return defaultPrompt, nil
}

// === LLM I/O ==================================================================

// llmOutputV2 mirrors the JSON the prompt asks the LLM to produce.
type llmOutputV2 struct {
	Action      string      `json:"action"` // "append" | "create" | "create_topic"
	TargetPath  string      `json:"target_path"`
	Title       string      `json:"title"`
	Category    string      `json:"category"`
	Tags        []string    `json:"tags"`
	Related     []string    `json:"related"`
	Body        string      `json:"body"`
	IndexUpdate IndexUpdate `json:"index_update"`
}

type IndexUpdate struct {
	Path     string `json:"path"`
	AddEntry string `json:"add_entry"`
}

var jsonBlockRe = regexp.MustCompile(`(?s)\{.*\}`)

// parseLLMOutput tolerates LLMs that wrap their JSON in ```json fences or
// add a preamble like "Here's the result:".
func parseLLMOutput(raw string) (*llmOutputV2, error) {
	raw = strings.TrimSpace(raw)
	var out llmOutputV2
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return &out, nil
	}
	m := jsonBlockRe.FindString(raw)
	if m == "" {
		return nil, fmt.Errorf("no json object found")
	}
	if err := json.Unmarshal([]byte(m), &out); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	return &out, nil
}

// === helpers ==================================================================

// composeTargetPath builds notes/<category>/<title>.md when the LLM didn't
// give us an explicit target_path. category may be slash-separated.
func (e *Engine) composeTargetPath(p *llmOutputV2) string {
	if p.Title == "" {
		return ""
	}
	cat := strings.Trim(p.Category, "/")
	title := safeFilename(p.Title)
	if cat == "" {
		return path.Join(e.Cfg.NotesDir, title+".md")
	}
	return path.Join(e.Cfg.NotesDir, cat, title+".md")
}

// safeFilename strips characters that fail on common filesystems. Chinese
// characters are kept as-is.
func safeFilename(s string) string {
	bad := []string{`\`, `/`, `:`, `*`, `?`, `"`, `<`, `>`, `|`, "\x00"}
	for _, c := range bad {
		s = strings.ReplaceAll(s, c, "_")
	}
	s = strings.Trim(s, " .")
	if s == "" {
		s = "untitled"
	}
	return s
}

type slimHit struct {
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Tags     []string `json:"tags,omitempty"`
	Category string   `json:"category,omitempty"`
}

func simplifyHits(hits []retrieval.Hit) []slimHit {
	out := make([]slimHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, slimHit{Path: h.Path, Title: h.Title, Tags: h.Tags, Category: h.Category})
	}
	return out
}

func defaultStr(v string, def string) string {
	if v == "" {
		return def
	}
	return v
}

// bumpUpdatedTimestamp updates the `updated:` field in YAML frontmatter, or
// adds it if missing. If the file has no frontmatter, returns it unchanged.
func bumpUpdatedTimestamp(content, ts string) string {
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	rest := content[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return content
	}
	header := rest[:end]
	body := rest[end+5:]

	updatedRe := regexp.MustCompile(`(?m)^updated:.*$`)
	if updatedRe.MatchString(header) {
		header = updatedRe.ReplaceAllString(header, "updated: "+ts)
	} else {
		header = strings.TrimRight(header, "\n") + "\nupdated: " + ts
	}
	return "---\n" + header + "\n---\n" + body
}

// defaultPrompt is the bundled fallback when the user has no vault prompt
// file. It mirrors scripts/templates/prompts/tidy.md (kept short here; the
// full version lives in the vault and is user-editable).
const defaultPrompt = `你是 knowLib 的知识整理助手。把零散笔记整理成结构化的知识库。

## 决策优先级
1. 看「现有笔记目录结构」,判断新内容是否属于已有主题
2. 优先 append 到已有笔记;只在内容独立且有深度时 create;尽量不 create_topic
3. 同主题的多个知识点应该合并到一篇笔记的不同章节,而不是每个一篇

## 目录结构规则
- notes/<主题>.md          ← 主题入口(索引页)
- notes/<主题>/<子笔记>.md  ← 详细笔记
- 最多 2 层

## 笔记格式
- frontmatter: title, category, tags, related, created, updated
- H1 是笔记标题(只有一个)
- H2 是主要章节
- H3 是子章节
- 不用 H4 及以下

## 输出 JSON
{
  "action": "append" | "create" | "create_topic",
  "target_path": "notes/.../xxx.md",
  "title": "...",
  "category": "...",
  "tags": [...],
  "related": ["[[...]]"],
  "body": "整理后的 markdown 内容",
  "index_update": {"path": "notes/<主题>.md", "add_entry": "- [[子笔记]] — 描述"}
}

严格约束:回复必须是合法 JSON,不编造内容,保持用户原意。
`
