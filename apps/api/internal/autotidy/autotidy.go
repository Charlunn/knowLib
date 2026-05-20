// Package autotidy schedules automatic tidy runs.
//
// Two trigger types, both controlled by user settings:
//
//   - Threshold: every minute we check inbox size; if >= settings.threshold,
//     fire a tidy-all.
//   - Schedule: cron-like spec (e.g. "0 2 * * *" = 02:00 daily). We use a
//     simple internal parser supporting standard 5-field cron.
//
// Both can be active simultaneously. Settings.AutoTidyEnabled is the master
// switch. If disabled, no triggers fire regardless of threshold/cron values.
package autotidy

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/charlunn/knowlib/api/internal/config"
	"github.com/charlunn/knowlib/api/internal/httpx"
	"github.com/charlunn/knowlib/api/internal/store"
	"github.com/charlunn/knowlib/api/internal/vault"
)

type Scheduler struct {
	cfg   *config.Config
	store *store.Store
	vault *vault.Vault
	tidy  *httpx.TidyClient

	mu          sync.Mutex
	lastFired   time.Time // dedup: don't fire two triggers within 1 minute
	cancelFn    context.CancelFunc
	stopWaitCh  chan struct{}
}

func New(cfg *config.Config, st *store.Store, v *vault.Vault, tidy *httpx.TidyClient) *Scheduler {
	return &Scheduler{
		cfg:   cfg,
		store: st,
		vault: v,
		tidy:  tidy,
	}
}

// Start runs the scheduler in the background. Cancel via Stop().
func (s *Scheduler) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFn = cancel
	s.stopWaitCh = make(chan struct{})

	go s.loop(ctx)
}

func (s *Scheduler) Stop() {
	if s.cancelFn != nil {
		s.cancelFn()
		<-s.stopWaitCh
	}
}

func (s *Scheduler) loop(ctx context.Context) {
	defer close(s.stopWaitCh)

	// Tick every minute; cron resolution is 1 min.
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	// Run an immediate check on startup so users don't wait 60s.
	s.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	defaults := store.Settings{
		LLMBaseURL: s.cfg.OpenAIBaseURL, LLMModel: s.cfg.OpenAIModel,
		LLMAPIKey:    s.cfg.OpenAIAPIKey,
		EmbedBaseURL: s.cfg.EmbedBaseURL, EmbedModel: s.cfg.EmbedModel,
		TidyTopK: s.cfg.TidyTopK, TidyMaxTokens: s.cfg.TidyMaxTokens,
	}
	st, err := s.store.GetSettings(defaults)
	if err != nil {
		return
	}
	if !st.AutoTidyEnabled {
		return
	}

	now := time.Now()
	// 1-minute dedup.
	s.mu.Lock()
	if now.Sub(s.lastFired) < time.Minute {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	shouldFire := false
	reason := ""

	// Threshold trigger.
	if st.AutoTidyThreshold > 0 {
		listing, err := s.vault.ListInboxAndRoot()
		if err == nil && len(listing) >= st.AutoTidyThreshold {
			shouldFire = true
			reason = "threshold"
		}
	}

	// Cron trigger.
	if !shouldFire && st.AutoTidyCronSpec != "" {
		if cronMatches(st.AutoTidyCronSpec, now) {
			shouldFire = true
			reason = "schedule"
		}
	}

	if !shouldFire {
		return
	}

	s.mu.Lock()
	s.lastFired = now
	s.mu.Unlock()

	log.Printf("autotidy: firing (reason=%s)", reason)

	// Fire a tidy-all with current settings as overrides.
	overrides := &httpx.TidyOverrides{
		LLMBaseURL:   st.LLMBaseURL,
		LLMAPIKey:    st.LLMAPIKey,
		LLMModel:     st.LLMModel,
		EmbedBaseURL: st.EmbedBaseURL,
		EmbedModel:   st.EmbedModel,
		TopK:         st.TidyTopK,
		MaxTokens:    st.TidyMaxTokens,
		PromptInline: st.TidyPrompt,
	}
	tidyCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	res, err := s.tidy.Run(tidyCtx, httpx.TidyRequest{All: true, Overrides: overrides})
	if err != nil {
		log.Printf("autotidy: tidy run failed: %v", err)
		return
	}
	log.Printf("autotidy: completed job=%s items=%d", res.JobID, len(res.Items))
}

// cronMatches reports whether `now` matches the standard 5-field cron spec.
// Fields: minute (0-59), hour (0-23), day-of-month (1-31), month (1-12), day-of-week (0-6, 0=Sun).
// Each field supports: '*', a single number, or a comma list (e.g. "1,15,30").
// Step '/N' and ranges 'A-B' are NOT supported (kept simple; users mostly want "0 2 * * *").
func cronMatches(spec string, now time.Time) bool {
	parts := splitFields(spec)
	if len(parts) != 5 {
		return false
	}
	if !cronFieldMatch(parts[0], now.Minute()) {
		return false
	}
	if !cronFieldMatch(parts[1], now.Hour()) {
		return false
	}
	if !cronFieldMatch(parts[2], now.Day()) {
		return false
	}
	if !cronFieldMatch(parts[3], int(now.Month())) {
		return false
	}
	if !cronFieldMatch(parts[4], int(now.Weekday())) {
		return false
	}
	return true
}

func splitFields(spec string) []string {
	var out []string
	cur := ""
	for _, r := range spec {
		if r == ' ' || r == '\t' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func cronFieldMatch(field string, value int) bool {
	if field == "*" || field == "" {
		return true
	}
	for _, part := range splitOn(field, ',') {
		n, ok := atoi(part)
		if ok && n == value {
			return true
		}
	}
	return false
}

func splitOn(s string, sep rune) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == sep {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, cur)
	return out
}

func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}
