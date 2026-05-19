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

func NewEngine(cfg *config.Config) *Engine {
	return &Engine{
		Cfg:     cfg,
		FS:      vaultfs.New(cfg.VaultPath, cfg.InboxDir, cfg.NotesDir, cfg.AtlasDir, cfg.KnowlibDir),
		Retr:    retrieval.New(cfg.EmbedBaseURL, cfg.EmbedModel, cfg.QdrantURL, cfg.QdrantCollection),
		LLM:     llm.New(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel),
		NowFunc: time.Now,
	}
}

// Run executes a tidy job. If req.All is true, runs over every inbox/ file.
func (e *Engine) Run(ctx context.Context, paths []string, all bool) (*Result, error) {
	if all {
		got, err := e.FS.ListInbox()
		if err != nil {
			return nil, err
		}
		paths = got
	}
	job := &Result{JobID: fmt.Sprintf("tidy-%d", e.NowFunc().Unix())}
	for _, p := range paths {
		item := e.runOne(ctx, p)
		job.Items = append(job.Items, item)
	}
	return job, nil
}

// runOne handles a single inbox file. Failure of the LLM or safediff causes
// us to leave the inbox file in place — the user can retry later.
func (e *Engine) runOne(ctx context.Context, srcPath string) Item {
	out := Item{SourcePath: srcPath, Status: "failed"}

	raw, err := e.FS.Read(srcPath)
	if err != nil {
		out.Reason = "read: " + err.Error()
		return out
	}
	srcFM, body := fmatter.Parse(string(raw))

	// 1. retrieve top-K related notes (best-effort: empty list on failure)
	hits, _ := e.Retr.Search(ctx, body, e.Cfg.TopK)
	relatedJSON, _ := json.MarshalIndent(simplifyHits(hits), "", "  ")

	// 2. load prompt template (vault file is authoritative; falls back to bundled default)
	prompt, err := e.loadPrompt()
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
	rawResp, err := e.LLM.Chat(ctx, prompt, user, e.Cfg.MaxTokens)
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
		"tidied_by":   e.Cfg.OpenAIModel,
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
		srcPath, target, e.Cfg.OpenAIModel, bodyAccepted,
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

func (e *Engine) loadPrompt() (string, error) {
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
