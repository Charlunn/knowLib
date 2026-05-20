// Package aiops handles batch AI operations on existing notes:
// deep_tidy, polish_logic, rewrite, knowledge_check.
//
// Unlike the inbox tidy flow (one input → one output), these operations
// take multiple notes as context and may produce multiple write/rename/delete
// ops. The caller decides whether to preview (return diff only) or apply.
package aiops

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charlunn/knowlib/tidy/internal/llm"
	"github.com/charlunn/knowlib/tidy/internal/vaultfs"
)

// Action is one of the supported AI operations.
type Action string

const (
	ActionDeepTidy       Action = "deep_tidy"
	ActionPolishLogic    Action = "polish_logic"
	ActionRewrite        Action = "rewrite"
	ActionKnowledgeCheck Action = "knowledge_check"
)

// Request is what the api forwards to the tidy worker.
type Request struct {
	Action      Action            `json:"action"`
	Paths       []string          `json:"paths,omitempty"`
	Folder      string            `json:"folder,omitempty"`
	PreviewOnly bool              `json:"preview_only"`
	Overrides   *RequestOverrides `json:"overrides,omitempty"`
}

type RequestOverrides struct {
	LLMBaseURL string `json:"llm_base_url,omitempty"`
	LLMAPIKey  string `json:"llm_api_key,omitempty"`
	LLMModel   string `json:"llm_model,omitempty"`
	MaxTokens  int    `json:"max_tokens,omitempty"`
	// Per-action prompt overrides (rare; usually loaded from vault file).
	PromptInline string `json:"prompt_inline,omitempty"`
}

// Operation describes a single change to be applied to the vault.
type Operation struct {
	Type    string `json:"type"` // create | modify | rename | delete
	Path    string `json:"path,omitempty"`
	From    string `json:"from,omitempty"`     // for rename
	To      string `json:"to,omitempty"`       // for rename
	Content string `json:"content,omitempty"`  // markdown body (no frontmatter)
	Reason  string `json:"reason,omitempty"`   // for delete
	Before  string `json:"before,omitempty"`   // pre-existing content (for diff display)
	Summary string `json:"summary,omitempty"`  // optional per-op note
	Issues  []any  `json:"issues,omitempty"`   // for knowledge_check
}

// Response is what the worker returns.
type Response struct {
	Action     Action      `json:"action"`
	Summary    string      `json:"summary"`
	Operations []Operation `json:"operations"`
}

// Engine wires the dependencies needed to run an AI op.
type Engine struct {
	FS         *vaultfs.FS
	LLM        *llm.Client
	PromptDir  string // e.g. /data/vault/.knowlib/prompts
}

func New(fs *vaultfs.FS, llm *llm.Client, promptDir string) *Engine {
	return &Engine{FS: fs, LLM: llm, PromptDir: promptDir}
}

// Run executes the request. If PreviewOnly, returns operations without applying.
// If !PreviewOnly, applies operations and returns the same operations as audit log.
func (e *Engine) Run(ctx context.Context, req Request) (*Response, error) {
	// 1. Resolve input paths.
	paths, err := e.resolvePaths(req)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no input paths")
	}

	// 2. Read all input notes.
	type inputNote struct {
		Path    string
		Content string
	}
	notes := make([]inputNote, 0, len(paths))
	for _, p := range paths {
		body, err := e.FS.Read(p)
		if err != nil {
			continue // best-effort: skip unreadable files
		}
		notes = append(notes, inputNote{Path: p, Content: string(body)})
	}

	// 3. Build LLM input.
	var sb strings.Builder
	for i, n := range notes {
		fmt.Fprintf(&sb, "\n=== 输入笔记 %d ===\n路径: %s\n内容:\n%s\n", i+1, n.Path, n.Content)
	}

	// 4. Load prompt for this action.
	prompt, err := e.loadPromptFor(req.Action, req.Overrides)
	if err != nil {
		return nil, err
	}

	// 5. Call LLM.
	llmClient := e.LLM
	if req.Overrides != nil &&
		(req.Overrides.LLMBaseURL != "" || req.Overrides.LLMAPIKey != "" || req.Overrides.LLMModel != "") {
		baseURL := req.Overrides.LLMBaseURL
		apiKey := req.Overrides.LLMAPIKey
		model := req.Overrides.LLMModel
		if baseURL == "" {
			baseURL = e.LLM.BaseURL
		}
		if apiKey == "" {
			apiKey = e.LLM.APIKey
		}
		if model == "" {
			model = e.LLM.Model
		}
		llmClient = llm.New(baseURL, apiKey, model)
	}
	maxTokens := 8192
	if req.Overrides != nil && req.Overrides.MaxTokens > 0 {
		maxTokens = req.Overrides.MaxTokens
	}

	rawResp, err := llmClient.Chat(ctx, prompt, sb.String(), maxTokens)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}

	// 6. Parse response.
	parsed, err := parseAIOpsJSON(rawResp)
	if err != nil {
		return nil, fmt.Errorf("parse: %w (raw len=%d)", err, len(rawResp))
	}
	parsed.Action = req.Action

	// 7. Enrich operations with `before` content for diff display.
	for i, op := range parsed.Operations {
		switch op.Type {
		case "modify", "delete":
			if before, err := e.FS.Read(op.Path); err == nil {
				parsed.Operations[i].Before = string(before)
			}
		case "rename":
			if before, err := e.FS.Read(op.From); err == nil {
				parsed.Operations[i].Before = string(before)
			}
		}
	}

	// 8. If preview only, return without applying.
	if req.PreviewOnly {
		return parsed, nil
	}

	// 9. Apply operations.
	if err := e.apply(parsed.Operations); err != nil {
		return parsed, fmt.Errorf("apply: %w", err)
	}
	return parsed, nil
}

// resolvePaths expands req.Folder (if set) into a list of .md paths under it.
// req.Paths is used as-is. Both can be combined.
func (e *Engine) resolvePaths(req Request) ([]string, error) {
	seen := map[string]bool{}
	var out []string

	for _, p := range req.Paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}

	if req.Folder != "" {
		abs, err := e.FS.Resolve(req.Folder)
		if err != nil {
			return nil, fmt.Errorf("folder: %w", err)
		}
		_ = filepath.WalkDir(abs, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if strings.HasPrefix(name, ".") && p != abs {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			rel, err := filepath.Rel(e.FS.Root, p)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if !seen[rel] {
				seen[rel] = true
				out = append(out, rel)
			}
			return nil
		})
	}

	return out, nil
}

// ApplyOperations executes a previously-generated set of operations without
// re-calling the LLM. Used by /ai-action/apply when the user confirms a
// preview returned by /ai-action.
func (e *Engine) ApplyOperations(ops []Operation) error {
	return e.apply(ops)
}

// apply executes the operations against the filesystem.
func (e *Engine) apply(ops []Operation) error {
	for _, op := range ops {
		switch op.Type {
		case "create":
			if op.Path == "" {
				continue
			}
			if err := e.FS.WriteAtomic(op.Path, []byte(op.Content)); err != nil {
				return fmt.Errorf("create %s: %w", op.Path, err)
			}
		case "modify":
			if op.Path == "" {
				continue
			}
			if err := e.FS.WriteAtomic(op.Path, []byte(op.Content)); err != nil {
				return fmt.Errorf("modify %s: %w", op.Path, err)
			}
		case "rename":
			if op.From == "" || op.To == "" {
				continue
			}
			// Read source, write to destination, delete source.
			body, err := e.FS.Read(op.From)
			if err != nil {
				return fmt.Errorf("rename read %s: %w", op.From, err)
			}
			finalContent := string(body)
			if op.Content != "" {
				finalContent = op.Content
			}
			if err := e.FS.WriteAtomic(op.To, []byte(finalContent)); err != nil {
				return fmt.Errorf("rename write %s: %w", op.To, err)
			}
			if err := e.FS.Remove(op.From); err != nil {
				return fmt.Errorf("rename remove %s: %w", op.From, err)
			}
		case "delete":
			if op.Path == "" {
				continue
			}
			if err := e.FS.Remove(op.Path); err != nil {
				return fmt.Errorf("delete %s: %w", op.Path, err)
			}
		}
	}
	return nil
}

func (e *Engine) loadPromptFor(action Action, overrides *RequestOverrides) (string, error) {
	if overrides != nil && overrides.PromptInline != "" {
		return overrides.PromptInline, nil
	}
	filename := ""
	switch action {
	case ActionDeepTidy:
		filename = "deep_tidy.md"
	case ActionPolishLogic, ActionRewrite:
		filename = "polish.md"
	case ActionKnowledgeCheck:
		filename = "knowledge_check.md"
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
	if e.PromptDir != "" {
		path := filepath.Join(e.PromptDir, filename)
		if b, err := os.ReadFile(path); err == nil {
			s := string(b)
			// For polish.md, append a line specifying which mode.
			if action == ActionPolishLogic {
				s += "\n\n## 当前模式\n\n请使用 polish_logic 模式。"
			} else if action == ActionRewrite {
				s += "\n\n## 当前模式\n\n请使用 rewrite 模式。"
			}
			return s, nil
		}
	}
	return defaultPromptFor(action), nil
}

func defaultPromptFor(action Action) string {
	switch action {
	case ActionDeepTidy:
		return defaultDeepTidyPrompt
	case ActionPolishLogic:
		return defaultPolishPrompt + "\n\n请使用 polish_logic 模式。"
	case ActionRewrite:
		return defaultPolishPrompt + "\n\n请使用 rewrite 模式。"
	case ActionKnowledgeCheck:
		return defaultKnowledgeCheckPrompt
	}
	return ""
}

var jsonBlockRe = regexp.MustCompile(`(?s)\{.*\}`)

func parseAIOpsJSON(raw string) (*Response, error) {
	raw = strings.TrimSpace(raw)
	var out Response
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

// === bundled defaults (fallback when prompt files missing) ===================

const defaultDeepTidyPrompt = `深度整理:把多篇相关笔记重组为连贯的知识结构。
合并同类项、重排结构、统一颗粒度、加 wikilink、保持原意。
输出 JSON: {summary, operations:[{type:create|modify|rename|delete, path|from|to, content, reason}]}`

const defaultPolishPrompt = `润色笔记:保持用户写作风格,只修正结构和明显错误。
不改变事实、观点、结论。
输出 JSON: {operations:[{type:modify, path, content, summary}]}`

const defaultKnowledgeCheckPrompt = `知识审查:逐条检查事实陈述,修正错误,保持原文风格。
拿不准的标 uncertain 不改。过时内容标 outdated。
输出 JSON: {operations:[{type:modify, path, content, issues:[{kind, original, fixed|note, explanation}]}]}`
