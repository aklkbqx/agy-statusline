package hud

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func f64Ptr(v float64) *float64 { return &v }
func i64Ptr(v int64) *int64     { return &v }

func writeMockAgySwap(t *testing.T, now time.Time) string {
	t.Helper()
	dir := t.TempDir()

	cfg := map[string]any{
		"schema": 1,
		"policy": map[string]any{
			"name":              "sticky",
			"min_remaining_pct": 10,
		},
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), cfgBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	obs := now.Add(-1 * time.Minute).Format(time.RFC3339)
	futureReset5h := now.Add(2*time.Hour + 4*time.Minute).Format(time.RFC3339)
	futureReset7d := now.Add(6*24*time.Hour + 11*time.Hour).Format(time.RFC3339)
	cooldownUntil := now.Add(2*24*time.Hour + 21*time.Hour).Format(time.RFC3339)

	makeGroups := func(tierID, tierName string, gem5h, gem7d, p3_5h, p3_7d float64) map[string]any {
		return map[string]any{
			"observed_at": obs,
			"tier":        map[string]any{"id": tierID, "name": tierName},
			"groups": []map[string]any{
				{
					"id":   "gemini",
					"name": "Gemini Models",
					"buckets": []map[string]any{
						{"id": "gemini-weekly", "name": "Weekly", "remaining_fraction": gem7d, "reset_at": futureReset7d, "window": "weekly"},
						{"id": "gemini-5h", "name": "5h", "remaining_fraction": gem5h, "reset_at": futureReset5h, "window": "5h"},
					},
				},
				{
					"id":   "third_party",
					"name": "Claude and GPT models",
					"buckets": []map[string]any{
						{"id": "3p-weekly", "name": "Weekly", "remaining_fraction": p3_7d, "reset_at": futureReset7d, "window": "weekly"},
						{"id": "3p-5h", "name": "5h", "remaining_fraction": p3_5h, "reset_at": futureReset5h, "window": "5h"},
					},
				},
			},
		}
	}

	accounts := map[string]any{
		"alex@example.com": map[string]any{
			"email":          "alex@example.com",
			"name":           "Studio Ultra",
			"secret_ref":     "vault:1",
			"quota_snapshot": makeGroups("g1-ultra-tier", "Google AI Ultra", 0.09, 0.47, 1.0, 1.0),
		},
		"sam@example.com": map[string]any{
			"email":          "sam@example.com",
			"name":           "Dev Pro",
			"secret_ref":     "vault:2",
			"quota_snapshot": makeGroups("g1-pro-tier", "Google AI Pro", 1.0, 1.0, 1.0, 1.0),
		},
		"riley@example.com": map[string]any{
			"email":          "riley@example.com",
			"name":           "Ship Pro",
			"secret_ref":     "vault:3",
			"quota_snapshot": makeGroups("g1-pro-tier", "Google AI Pro", 1.0, 1.0, 1.0, 1.0),
		},
		"morgan@example.com": map[string]any{
			"email":          "morgan@example.com",
			"name":           "Build Pro",
			"secret_ref":     "vault:4",
			"quota_snapshot": makeGroups("g1-pro-tier", "Google AI Pro", 0.953, 0.987, 1.0, 1.0),
		},
		"jordan@example.com": map[string]any{
			"email":      "jordan@example.com",
			"name":       "Lab Ultra",
			"secret_ref": "vault:5",
			"quota_limits": map[string]any{
				"gemini-4-carbon-high": map[string]any{
					"family":   "gemini",
					"reset_at": cooldownUntil,
				},
			},
			"quota_snapshot": makeGroups("g1-ultra-tier", "Google AI Ultra", 1.0, 1.0, 1.0, 1.0),
		},
	}

	accBytes, err := json.Marshal(accounts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), accBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFullWidthExactJustificationAndFleetDeck(t *testing.T) {
	now := time.Date(2026, 11, 18, 15, 55, 0, 0, time.UTC)
	swapDir := writeMockAgySwap(t, now)
	stateDir := t.TempDir()

	data := &StatusLineData{
		ConversationID:    "conv-142",
		ConversationTitle: "Cockpit V2",
		Email:             "alex@example.com",
		TerminalWidth:     142,
		Model:             &ModelInfo{ID: "gemini-pro-orchestrator", DisplayName: "Orchestrator (Carbon)"},
		Workspace:         &WorkspaceInfo{CurrentDir: "/Users/demo", ProjectDir: "/Users/demo"},
		VCS:               &VCSInfo{Type: "git", Branch: "main", Dirty: true},
		ContextWindow: &ContextInfo{
			ContextWindowSize: 1_000_000,
			UsedPercentage:    f64Ptr(28),
			CurrentUsage:      &CurrentUsageInfo{InputTokens: 40_000, CacheReadInputTokens: 214_000},
		},
		Quota: map[string]QuotaBucket{
			"gemini-5h":     {RemainingFraction: f64Ptr(0.09), ResetInSeconds: i64Ptr(2*3600 + 4*60)},
			"gemini-weekly": {RemainingFraction: f64Ptr(0.47), ResetInSeconds: i64Ptr(6*86400 + 11*3600)},
			"3p-5h":         {RemainingFraction: f64Ptr(1.0), ResetInSeconds: i64Ptr(5 * 3600)},
			"3p-weekly":     {RemainingFraction: f64Ptr(1.0), ResetInSeconds: i64Ptr(7 * 86400)},
		},
		Cost: &CostInfo{TotalUSD: 0.42},
	}

	rendered := Render(data, now, envMap(map[string]string{
		"AGY_SWAP_DIR":             swapDir,
		"AGY_STATUSLINE_STATE_DIR": stateDir,
		"HOME":                     "/Users/demo",
		"TERM":                     "xterm-256color",
	}))
	lines := strings.Split(rendered, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines at 142 cols, got %d:\n%s", len(lines), stripANSI(rendered))
	}

	line1 := stripANSI(lines[0])
	line2 := stripANSI(lines[1])

	if w := DisplayWidth(line1); w != 142 {
		t.Fatalf("line 1 must justify to exact width 142, got %d: %q", w, line1)
	}
	if w := DisplayWidth(line2); w != 142 {
		t.Fatalf("line 2 must justify to exact width 142, got %d: %q", w, line2)
	}

	for _, want := range []string{"⬢ Studio Ultra", "ULTRA", "GEM 5h", "9%", "↻2h04m", "7d", "47%", "3P 100%", "CTX", "28%", "254k/1M", "⌁", "⌂ ~", "⎇ main"} {
		if !strings.Contains(line1, want) {
			t.Fatalf("line 1 missing %q in %q", want, line1)
		}
	}
	for _, want := range []string{"Dev Pro", "Ship Pro", "Build Pro", "Lab Ultra", "◷2d21h", "3P 100%", "SWAP READY ▸ Dev Pro (3/4)"} {
		if !strings.Contains(line2, want) {
			t.Fatalf("line 2 missing %q in %q", want, line2)
		}
	}
	if strings.Contains(line1+line2, "@example.com") {
		t.Fatalf("never expose full emails: %q / %q", line1, line2)
	}
}

func TestClaudeModelPromotesPartialAccountInFleet(t *testing.T) {
	now := time.Date(2026, 11, 18, 15, 55, 0, 0, time.UTC)
	swapDir := writeMockAgySwap(t, now)
	stateDir := t.TempDir()

	data := &StatusLineData{
		Email:         "alex@example.com",
		TerminalWidth: 142,
		Model:         &ModelInfo{ID: "claude-opus-5-5", DisplayName: "Claude Opus 5.5"},
		ContextWindow: &ContextInfo{ContextWindowSize: 250_000, UsedPercentage: f64Ptr(38)},
		Quota: map[string]QuotaBucket{
			"gemini-5h":     {RemainingFraction: f64Ptr(0.09), ResetInSeconds: i64Ptr(3600)},
			"gemini-weekly": {RemainingFraction: f64Ptr(0.47), ResetInSeconds: i64Ptr(86400)},
			"3p-5h":         {RemainingFraction: f64Ptr(0.82), ResetInSeconds: i64Ptr(4 * 3600)},
			"3p-weekly":     {RemainingFraction: f64Ptr(0.95), ResetInSeconds: i64Ptr(6 * 86400)},
		},
	}

	rendered := Render(data, now, envMap(map[string]string{
		"AGY_SWAP_DIR":             swapDir,
		"AGY_STATUSLINE_STATE_DIR": stateDir,
		"NO_COLOR":                 "1",
	}))
	lines := strings.Split(rendered, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "3P 5h") || !strings.Contains(lines[0], "82%") {
		t.Fatalf("expected 3P foregrounded on row 1, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "FLEET 4/4 READY") {
		t.Fatalf("expected Lab Ultra promoted to ready (4/4 READY) for 3P, got %q", lines[1])
	}
}

func TestHybridAnimationsAndDeltaTracker(t *testing.T) {
	t0 := time.Date(2026, 11, 18, 15, 55, 0, 0, time.UTC)
	swapDir := writeMockAgySwap(t, t0)
	stateDir := t.TempDir()

	data := &StatusLineData{
		ConversationID: "conv-anim",
		Email:          "alex@example.com",
		TerminalWidth:  142,
		AgentState:     "thinking",
		Model:          &ModelInfo{ID: "gemini-pro-orchestrator"},
		ContextWindow: &ContextInfo{
			ContextWindowSize: 200_000,
			UsedPercentage:    f64Ptr(10),
			CurrentUsage:      &CurrentUsageInfo{InputTokens: 20_000},
		},
		Quota: map[string]QuotaBucket{
			"gemini-5h":     {RemainingFraction: f64Ptr(0.09), ResetInSeconds: i64Ptr(7400)},
			"gemini-weekly": {RemainingFraction: f64Ptr(0.47), ResetInSeconds: i64Ptr(500000)},
		},
		Subagents: []SubagentInfo{{Name: "sub-1", Status: "running"}},
	}

	env := envMap(map[string]string{
		"AGY_SWAP_DIR":             swapDir,
		"AGY_STATUSLINE_STATE_DIR": stateDir,
		"TERM":                     "xterm-256color",
	})

	r0 := Render(data, t0, env)
	t1 := t0.Add(300 * time.Millisecond)
	data.ContextWindow.UsedPercentage = f64Ptr(18)
	data.ContextWindow.CurrentUsage.InputTokens = 35_000
	r1 := Render(data, t1, env)

	if r0 == r1 {
		t.Fatalf("expected active animation frames to differ across 300ms")
	}
	if !strings.Contains(stripANSI(r1), "THINKING") {
		t.Fatalf("expected THINKING shimmer label, got %q", stripANSI(r1))
	}
	if !strings.Contains(stripANSI(r1), "+15k") {
		t.Fatalf("expected +15k live token delta badge, got %q", stripANSI(r1))
	}

	data.AgentState = "idle"
	idle := Render(data, t1, env)
	if !strings.Contains(idle, ";5;") && !strings.Contains(idle, "\x1b[5m") {
		t.Fatalf("expected SGR 5 pulse on critical 9%% quota / top swap target, got %q", idle)
	}

	noBlinkEnv := envMap(map[string]string{
		"AGY_SWAP_DIR":             swapDir,
		"AGY_STATUSLINE_STATE_DIR": stateDir,
		"TERM":                     "xterm-256color",
		"AGY_STATUSLINE_NO_BLINK":  "1",
	})
	noBlink := Render(data, t1, noBlinkEnv)
	if strings.Contains(noBlink, ";5;") || strings.Contains(noBlink, "\x1b[5m") {
		t.Fatalf("expected no SGR 5 pulse when AGY_STATUSLINE_NO_BLINK=1, got %q", noBlink)
	}
}

func TestResponsiveWidthSweepWithThaiAndCJK(t *testing.T) {
	now := time.Date(2026, 11, 18, 15, 55, 0, 0, time.UTC)
	swapDir := writeMockAgySwap(t, now)
	stateDir := t.TempDir()

	if got := DisplayWidth("ก้าวหน้า"); got != 6 {
		t.Fatalf("expected Thai combining marks = 0 width (total 6), got %d", got)
	}

	data := &StatusLineData{
		ConversationID:    "conv-sweep",
		ConversationTitle: "ทดสอบระบบภาษาไทย",
		Email:             "alex@example.com",
		Model:             &ModelInfo{ID: "gemini-pro-orchestrator"},
		Workspace:         &WorkspaceInfo{CurrentDir: "/tmp/โครงการทดสอบ", ProjectDir: "/tmp/โครงการทดสอบ"},
		VCS:               &VCSInfo{Type: "git", Branch: "feat/ภาษาไทย-日本語-branch", Dirty: true},
		ContextWindow:     &ContextInfo{ContextWindowSize: 200_000, UsedPercentage: f64Ptr(62)},
		Quota: map[string]QuotaBucket{
			"gemini-5h":     {RemainingFraction: f64Ptr(0.18), ResetInSeconds: i64Ptr(2700)},
			"gemini-weekly": {RemainingFraction: f64Ptr(0.50), ResetInSeconds: i64Ptr(3 * 86400)},
			"3p-5h":         {RemainingFraction: f64Ptr(1.0)},
		},
		Cost: &CostInfo{TotalUSD: 1.25},
	}

	for _, cols := range []int{180, 142, 115, 90, 72, 56, 44, 28} {
		data.TerminalWidth = cols
		rendered := Render(data, now, envMap(map[string]string{
			"AGY_SWAP_DIR":             swapDir,
			"AGY_STATUSLINE_STATE_DIR": stateDir,
			"TERM":                     "xterm-256color",
		}))
		lines := strings.Split(rendered, "\n")
		if cols < 58 && len(lines) != 1 {
			t.Fatalf("width %d should collapse to 1 line, got %d", cols, len(lines))
		}
		if cols >= 58 && len(lines) != 2 {
			t.Fatalf("width %d should render 2 lines, got %d", cols, len(lines))
		}
		for idx, ln := range lines {
			plain := stripANSI(ln)
			w := DisplayWidth(plain)
			if w > cols {
				t.Fatalf("width %d line %d overflowed: got %d > %d (%q)", cols, idx+1, w, cols, plain)
			}
			if strings.Count(ln, "\x1b[") != 2*strings.Count(ln, "\x1b[0m") {
				t.Fatalf("width %d line %d has unbalanced ANSI escapes: %q", cols, idx+1, ln)
			}
		}
	}
}

func TestPrivacyAndLiveFleetQuotaMemory(t *testing.T) {
	now := time.Date(2026, 11, 18, 15, 55, 0, 0, time.UTC)
	swapDir := writeMockAgySwap(t, now)
	stateDir := t.TempDir()

	d1 := &StatusLineData{
		Email:         "alex@example.com",
		TerminalWidth: 142,
		Model:         &ModelInfo{ID: "gemini-pro-orchestrator"},
		ContextWindow: &ContextInfo{ContextWindowSize: 200_000, UsedPercentage: f64Ptr(15)},
		Quota: map[string]QuotaBucket{
			"gemini-5h":     {RemainingFraction: f64Ptr(0.09), ResetInSeconds: i64Ptr(7200)},
			"gemini-weekly": {RemainingFraction: f64Ptr(0.47), ResetInSeconds: i64Ptr(500000)},
			"3p-5h":         {RemainingFraction: f64Ptr(1.0)},
		},
	}
	env := envMap(map[string]string{
		"AGY_SWAP_DIR":             swapDir,
		"AGY_STATUSLINE_STATE_DIR": stateDir,
		"NO_COLOR":                 "1",
	})
	_ = Render(d1, now, env)

	d2 := &StatusLineData{
		Email:         "sam@example.com",
		TerminalWidth: 142,
		Model:         &ModelInfo{ID: "gemini-pro-orchestrator"},
		ContextWindow: &ContextInfo{ContextWindowSize: 200_000, UsedPercentage: f64Ptr(5)},
		Quota: map[string]QuotaBucket{
			"gemini-5h":     {RemainingFraction: f64Ptr(1.0), ResetInSeconds: i64Ptr(18000)},
			"gemini-weekly": {RemainingFraction: f64Ptr(1.0), ResetInSeconds: i64Ptr(604800)},
			"3p-5h":         {RemainingFraction: f64Ptr(1.0)},
		},
	}
	switched := Render(d2, now.Add(time.Minute), env)
	lines := strings.Split(switched, "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "Studio U") || !strings.Contains(lines[1], "9%") {
		t.Fatalf("expected fleet cache memory of Studio Ultra at 9%% on row 2, got %q", switched)
	}

	privEnv := envMap(map[string]string{
		"AGY_SWAP_DIR":              swapDir,
		"AGY_STATUSLINE_STATE_DIR":  stateDir,
		"AGY_CLI_HIDE_ACCOUNT_INFO": "1",
		"NO_COLOR":                  "1",
	})
	priv := Render(d1, now, privEnv)
	for _, forbidden := range []string{"Studio", "Dev Pro", "Ship Pro", "Build Pro", "Lab Ultra", "⬢", "⇄", "FLEET"} {
		if strings.Contains(priv, forbidden) {
			t.Fatalf("privacy mode leaked %q in %q", forbidden, priv)
		}
	}
}

func TestThemesAndSingleLineOption(t *testing.T) {
	now := time.Date(2026, 11, 18, 15, 55, 0, 0, time.UTC)
	swapDir := writeMockAgySwap(t, now)
	stateDir := t.TempDir()

	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"colorScheme":"catppuccin"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveThemeName("", envMap(map[string]string{"AGY_SETTINGS_PATH": settingsPath})); got != "catppuccin" {
		t.Fatalf("expected catppuccin from settings colorScheme, got %q", got)
	}
	if got := ResolveThemeName("", envMap(map[string]string{
		"AGY_SETTINGS_PATH":    settingsPath,
		"AGY_STATUSLINE_THEME": "nord",
	})); got != "nord" {
		t.Fatalf("expected AGY_STATUSLINE_THEME to override settings, got %q", got)
	}

	data := &StatusLineData{
		Email:         "alex@example.com",
		TerminalWidth: 142,
		Model:         &ModelInfo{ID: "gemini-pro-orchestrator"},
		ContextWindow: &ContextInfo{ContextWindowSize: 200_000, UsedPercentage: f64Ptr(25)},
		Quota: map[string]QuotaBucket{
			"gemini-5h":     {RemainingFraction: f64Ptr(0.09), ResetInSeconds: i64Ptr(3600)},
			"gemini-weekly": {RemainingFraction: f64Ptr(0.47), ResetInSeconds: i64Ptr(86400)},
		},
	}

	catOut := Render(data, now, envMap(map[string]string{
		"AGY_SWAP_DIR":               swapDir,
		"AGY_STATUSLINE_STATE_DIR":   stateDir,
		"AGY_STATUSLINE_THEME":       "catppuccin",
		"AGY_STATUSLINE_SINGLE_LINE": "1",
		"TERM":                       "xterm-256color",
	}))
	if strings.Contains(catOut, "\n") {
		t.Fatalf("AGY_STATUSLINE_SINGLE_LINE=1 should output single line, got %q", catOut)
	}
	if !strings.Contains(stripANSI(catOut), "⇄3") {
		t.Fatalf("single-line mode with low quota should include inline ⇄3 badge, got %q", stripANSI(catOut))
	}
	// Catppuccin orange #fab387 -> (250, 179, 135)
	if !strings.Contains(catOut, "250;179;135") {
		t.Fatalf("expected Catppuccin RGB palette in output, got %q", catOut)
	}
}

func TestGitPorcelainProbe(t *testing.T) {
	repo := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-b", "main")
	_ = os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644)
	runGit("add", "a.txt")
	runGit("commit", "-m", "init")
	_ = os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644)
	_ = os.WriteFile(filepath.Join(repo, "b.txt"), []byte("staged\n"), 0o644)
	runGit("add", "b.txt")
	_ = os.WriteFile(filepath.Join(repo, "c.txt"), []byte("untracked\n"), 0o644)

	detail, ok := ProbeGitPorcelain(repo)
	if !ok {
		t.Fatal("expected ProbeGitPorcelain to succeed")
	}
	if detail.Branch != "main" || detail.Staged != 1 || detail.Modified != 1 || detail.Untracked != 1 || detail.IsWorktree {
		t.Fatalf("unexpected git detail: %+v", detail)
	}
}

func BenchmarkRender(b *testing.B) {
	now := time.Date(2026, 11, 18, 15, 55, 0, 0, time.UTC)
	dir := b.TempDir()
	stateDir := b.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"schema":1,"policy":{"min_remaining_pct":10}}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(`{
		"alex@example.com":{"email":"alex@example.com","name":"Studio Ultra","secret_ref":"v1"},
		"sam@example.com":{"email":"sam@example.com","name":"Dev Pro","secret_ref":"v2","quota_snapshot":{"groups":[{"id":"gemini","buckets":[{"id":"gemini-5h","remaining_fraction":1.0,"window":"5h"}]}]}}
	}`), 0o600)

	data := &StatusLineData{
		ConversationID: "bench",
		Email:          "alex@example.com",
		TerminalWidth:  142,
		AgentState:     "thinking",
		Model:          &ModelInfo{ID: "gemini-pro-orchestrator", DisplayName: "Orchestrator (Carbon)"},
		VCS:            &VCSInfo{Type: "git", Branch: "main", Dirty: false},
		ContextWindow:  &ContextInfo{ContextWindowSize: 1_000_000, UsedPercentage: f64Ptr(28)},
		Quota: map[string]QuotaBucket{
			"gemini-5h":     {RemainingFraction: f64Ptr(0.09), ResetInSeconds: i64Ptr(7440)},
			"gemini-weekly": {RemainingFraction: f64Ptr(0.47), ResetInSeconds: i64Ptr(558000)},
			"3p-5h":         {RemainingFraction: f64Ptr(1.0)},
		},
	}
	env := envMap(map[string]string{"AGY_SWAP_DIR": dir, "AGY_STATUSLINE_STATE_DIR": stateDir, "TERM": "xterm-256color"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Render(data, now, env)
	}
}
