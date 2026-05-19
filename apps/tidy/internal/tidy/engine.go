// Package tidy is the orchestrator: read inbox file → retrieve context → call
// LLM → safediff verify → write notes/.../<title>.md → maintain atlas/ → log.
package tidy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charlunn/knowlib/tidy/internal/atlas"
	"github.com/charlunn/knowlib/tidy/internal/config"
	"github.com/charlunn/knowlib/tidy/internal/fmatter"
	"github.com/charlunn/knowlib/tidy/internal/llm"
	"github.com/charlunn/knowlib/tidy/internal/retrieval"
	"github.com/charlunn/knowlib/tidy/internal/safediff"
	"github.com/charlunn/knowlib/tidy/internal/vaultfs"
)

type Item struct {
	SourcePath string `json:"source_path"`
	TargetPath string `json:"target_path,omitempty"`
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

// runtimeContext bundles the values that may be overridden per-request.
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
	// Only construct new clients when the override actually differs from defaults.
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

// Run executes a tidy job. If req.All is true, runs over every inbox/ file.
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

	// Bounded concurrency. The LLM is the bottleneck; 3 in-flight requests is
	// a safe default that respects most provider rate limits without leaving
	// the user staring at a serial queue. Single-item jobs skip the goroutines
	// entirely so we don't pay the orchestration cost for the common case.
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

// runOne handles a single inbox file. Failure of the LLM or safediff causes
// us to leave the inbox file in place — the user can retry later.
func (e *Engine) runOne(ctx context.Context, srcPath string, rc *runtimeContext) Item {
	out := Item{SourcePath: srcPath, Status: "failed"}

	raw, err := e.FS.Read(srcPath)
	if err != nil {
		out.Reason = "read: " + err.Error()
		return out
	}
	srcFM, body := fmatter.Parse(string(raw))

	// 1. retrieve top-K related notes (best-effort: empty list on failure)
	hits, _ := rc.retr.Search(ctx, body, rc.topK)
	relatedJSON, _ := json.MarshalIndent(simplifyHits(hits), "", "  ")

	// 2. load prompt: inline override > vault file > bundled default
	prompt, err := e.loadPrompt(rc.promptText)
	if err != nil {
		out.Reason = "prompt: " + err.Error()
		return out
	}

	// 3. ask LLM
	user := fmt.Sprintf(
		"=== 原文(inbox 文件内容)===\n%s\n\n=== 相关笔记列表 ===\n%s\n",
		body,
		string(relatedJSON),
	)
	rawResp, err := rc.llm.Chat(ctx, prompt, user, rc.maxTokens)
	if err != nil {
		out.Reason = "llm: " + err.Error()
		return out
	}
	parsed, err := parseLLMJSON(rawResp)
	if err != nil {
		out.Reason = "parse: " + err.Error()
		return out
	}

	// 4. safediff: verify body_with_links is wikilink-only modifications
	finalBody, diffErr := safediff.CheckBodyWithLinks(body, parsed.BodyWithLinks)
	bodyToWrite := finalBody
	bodyAccepted := diffErr == nil
	if !bodyAccepted {
		// Fall back to original body verbatim — frontmatter only.
		bodyToWrite = body
	}

	// 5. compose target path + frontmatter
	target := e.composeTargetPath(parsed)
	if target == "" {
		out.Reason = "no category/title in LLM output"
		return out
	}
	now := e.NowFunc().UTC().Format(time.RFC3339)
	keys := []string{"title", "category", "tags", "aliases", "related", "prereq", "postreq",
		"created", "tidied", "tidied_by", "source_path"}
	fm := map[string]any{
		"title":       parsed.Title,
		"category":    parsed.Category,
		"tags":        parsed.Tags,
		"aliases":     parsed.Aliases,
		"related":     parsed.Related,
		"prereq":      parsed.Prereq,
		"postreq":     parsed.Postreq,
		"created":     defaultStr(srcFM["captured_at"], now),
		"tidied":      now,
		"tidied_by":   rc.model,
		"source_path": srcPath,
	}
	rendered := fmatter.Render(keys, fm, bodyToWrite)

	// 6. write target file
	if err := e.FS.WriteAtomic(target, []byte(rendered)); err != nil {
		out.Reason = "write: " + err.Error()
		return out
	}

	// 7. update atlas (best-effort; do not fail the whole tidy)
	_, _ = atlas.Apply(e.FS, parsed.MOCUpdates)

	// 8. delete inbox source
	_ = e.FS.Remove(srcPath)

	// 9. log
	_ = e.FS.AppendLog("tidy.log", fmt.Sprintf(
		"%s -> %s (model=%s, body_accepted=%t)",
		srcPath, target, rc.model, bodyAccepted,
	))

	out.TargetPath = target
	if bodyAccepted {
		out.Status = "ok"
	} else {
		out.Status = "partial"
		out.Reason = "body_with_links rejected by safediff: " + diffErr.Error()
	}
	return out
}

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
	// Bundled fallback: keep the worker functional even if the user wiped the
	// prompt file. Should match scripts/templates/prompts/tidy.md spiritually.
	return defaultPrompt, nil
}

const defaultPrompt = `你是 knowLib 的整理助手。用户随手记的笔记进 inbox/,你负责归类、加 frontmatter、加 wikilinks,并维护知识图谱。

严格约束:
1. 不要修改用户原文的任何实质内容。只能在原词上包裹 [[wikilink]],不能改字、不能删句、不能换标点。代码会做 diff 校验,改了就拒绝。
2. wikilink 只在第一次出现时添加,同一术语重复出现只对第一次加。
3. 不编造前置/后置/相关笔记 —— 只能从给定的相关笔记列表里选。
4. category 用斜杠分隔(如 "学习/高等数学/微分方程"),不超过 4 层。
5. title 简洁明确,不超过 30 字。
6. 回复必须是合法 JSON,无前导/尾随说明文字。

输出 JSON 字段:title, category, tags, aliases, related, prereq, postreq, body_with_links, moc_updates。
related/prereq/postreq 用 wikilink 形式,如 "[[导数与微分]]"。
moc_updates 形如:[{"path": "atlas/高等数学.md", "section": "微分方程", "add_link": "[[一阶线性微分方程]]"}]。
`

// === LLM JSON parsing =========================================================

type llmOutput struct {
	Title         string         `json:"title"`
	Category      string         `json:"category"`
	Tags          []string       `json:"tags"`
	Aliases       []string       `json:"aliases"`
	Related       []string       `json:"related"`
	Prereq        []string       `json:"prereq"`
	Postreq       []string       `json:"postreq"`
	BodyWithLinks string         `json:"body_with_links"`
	MOCUpdates    []atlas.Update `json:"moc_updates"`
}

// parseLLMJSON tolerates LLMs that wrap their JSON in ```json fences or add
// preamble like "Here's the result:". We extract the first {...} block.
var jsonBlockRe = regexp.MustCompile(`(?s)\{.*\}`)

func parseLLMJSON(raw string) (*llmOutput, error) {
	raw = strings.TrimSpace(raw)
	// Try direct parse first.
	var out llmOutput
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return &out, nil
	}
	// Fallback: pull out first JSON object.
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

func (e *Engine) composeTargetPath(p *llmOutput) string {
	if p.Title == "" || p.Category == "" {
		return ""
	}
	cat := strings.Trim(p.Category, "/")
	title := safeFilename(p.Title)
	return path.Join(e.Cfg.NotesDir, cat, title+".md")
}

// safeFilename strips characters that fail on common filesystems (Windows
// especially). Keeps Chinese characters as-is — they're valid on every modern
// FS we'll deploy on.
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
