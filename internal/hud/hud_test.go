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
	r0Later := Render(data, t0.Add(300*time.Millisecond), env)
	if r0 != r0Later {
		t.Fatalf("expected steady deterministic output when payload is unchanged (no frozen frame-stepped shimmer)")
	}
	plain0 := stripANSI(r0)
	if !strings.Contains(plain0, "◆") || !strings.Contains(plain0, "THINKING") {
		t.Fatalf("expected active beacon ◆ and THINKING label, got %q", plain0)
	}
	if !strings.Contains(plain0, "◈1") || strings.ContainsAny(plain0, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏▁▃▅▇") {
		t.Fatalf("expected compact ◈1 badge without frozen Braille/equalizer glyphs, got %q", plain0)
	}
	if !strings.Contains(r0, ";5;") {
		t.Fatalf("expected SGR 5 hardware pulse on active ◆ beacon and ◈ subagent badge, got %q", r0)
	}

	t1 := t0.Add(300 * time.Millisecond)
	data.ContextWindow.UsedPercentage = f64Ptr(18)
	data.ContextWindow.CurrentUsage.InputTokens = 35_000
	r1 := Render(data, t1, env)

	if r0 == r1 {
		t.Fatalf("expected output to reflect token jump")
	}
	if !strings.Contains(stripANSI(r1), "+15k") {
		t.Fatalf("expected +15k live token delta badge, got %q", stripANSI(r1))
	}

	noBlinkEnv := envMap(map[string]string{
		"AGY_SWAP_DIR":             swapDir,
		"AGY_STATUSLINE_STATE_DIR": stateDir,
		"TERM":                     "xterm-256color",
		"AGY_STATUSLINE_NO_BLINK":  "1",
	})
	noBlinkWorking := Render(data, t1, noBlinkEnv)
	if strings.Contains(noBlinkWorking, ";5;") || strings.Contains(noBlinkWorking, "\x1b[5m") {
		t.Fatalf("expected no SGR 5 pulse while working when AGY_STATUSLINE_NO_BLINK=1, got %q", noBlinkWorking)
	}

	data.AgentState = "idle"
	idle := Render(data, t1, env)
	if !strings.Contains(idle, ";5;") && !strings.Contains(idle, "\x1b[5m") {
		t.Fatalf("expected SGR 5 pulse on critical 9%% quota / top swap target, got %q", idle)
	}

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

func TestFleetCountdownTicksLiveFromResetTime(t *testing.T) {
	t0 := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	swapDir := writeMockAgySwap(t, t0)
	stateDir := t.TempDir()
	env := envMap(map[string]string{
		"AGY_SWAP_DIR":             swapDir,
		"AGY_STATUSLINE_STATE_DIR": stateDir,
		"NO_COLOR":                 "1",
	})

	// Account alex hits low quota with a 2h reset window; saved to fleet.json.
	d1 := &StatusLineData{
		Email:         "alex@example.com",
		TerminalWidth: 142,
		Model:         &ModelInfo{ID: "gemini-pro-orchestrator"},
		ContextWindow: &ContextInfo{ContextWindowSize: 200_000, UsedPercentage: f64Ptr(15)},
		Quota: map[string]QuotaBucket{
			"gemini-5h":     {RemainingFraction: f64Ptr(0.05), ResetInSeconds: i64Ptr(2 * 3600)},
			"gemini-weekly": {RemainingFraction: f64Ptr(0.50), ResetInSeconds: i64Ptr(5 * 86400)},
			"3p-5h":         {RemainingFraction: f64Ptr(1.0)},
		},
	}
	_ = Render(d1, t0, env)

	// Verify persisted fleet.json clears frozen ResetInSeconds when ResetTime is stored.
	raw, err := os.ReadFile(filepath.Join(stateDir, "fleet.json"))
	if err != nil {
		t.Fatalf("expected fleet.json to exist: %v", err)
	}
	var persisted map[string]fleetCacheEntry
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("invalid fleet.json: %v", err)
	}
	b5 := persisted["alex@example.com"].Quota["gemini-5h"]
	if b5.ResetTime == nil || b5.ResetInSeconds != nil {
		t.Fatalf("fleet.json should store ResetTime without frozen ResetInSeconds, got %+v", b5)
	}

	// Simulate legacy fleet.json with a stale frozen ResetInSeconds alongside ResetTime.
	frozen := int64(2 * 3600)
	b5.ResetInSeconds = &frozen
	persisted["alex@example.com"].Quota["gemini-5h"] = b5
	legacyBytes, _ := json.Marshal(persisted)
	if err := os.WriteFile(filepath.Join(stateDir, "fleet.json"), legacyBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	// 40 minutes later, switch to sam: alex's countdown must show 1h20m, NOT frozen 2h00m.
	tLater := t0.Add(40 * time.Minute)
	d2 := &StatusLineData{
		Email:         "sam@example.com",
		TerminalWidth: 142,
		Model:         &ModelInfo{ID: "gemini-pro-orchestrator"},
		ContextWindow: &ContextInfo{ContextWindowSize: 200_000, UsedPercentage: f64Ptr(5)},
		Quota: map[string]QuotaBucket{
			"gemini-5h":     {RemainingFraction: f64Ptr(1.0), ResetInSeconds: i64Ptr(18000)},
			"gemini-weekly": {RemainingFraction: f64Ptr(1.0), ResetInSeconds: i64Ptr(604800)},
		},
	}
	rendered := Render(d2, tLater, env)
	if !strings.Contains(rendered, "◷1h20m") {
		t.Fatalf("expected live countdown ◷1h20m for cached reserve account, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "◷2h00m") {
		t.Fatalf("countdown stayed frozen at 2h00m:\n%s", rendered)
	}
}

func TestPreObservationResetTimeNotFalselyResetToFull(t *testing.T) {
	obs := time.Date(2026, 11, 19, 12, 42, 9, 0, time.UTC)
	now := time.Date(2026, 11, 19, 15, 0, 3, 0, time.UTC)

	preObsReset := time.Date(2026, 11, 19, 11, 8, 23, 0, time.UTC).Format(time.RFC3339)
	postObsReset := time.Date(2026, 11, 19, 14, 0, 0, 0, time.UTC).Format(time.RFC3339)

	raw := map[string]QuotaBucket{
		"gemini-5h": {RemainingFraction: f64Ptr(0.08), ResetTime: &preObsReset},
		"3p-5h":     {RemainingFraction: f64Ptr(0.08), ResetTime: &postObsReset},
	}
	norm := normalizeCachedBuckets(raw, obs, true, now)

	if got := bucketRemainingPct(norm["gemini-5h"]); got > 9.0 {
		t.Fatalf("bucket with reset_time before observed_at must NOT reset to 100%%, got %.1f%%", got)
	}
	if got := bucketRemainingPct(norm["3p-5h"]); got < 99.9 {
		t.Fatalf("bucket with reset_time between observed_at and now should reset to 100%%, got %.1f%%", got)
	}
}

func TestFleetStaleIndicator(t *testing.T) {
	now := time.Date(2026, 11, 19, 15, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	stateDir := t.TempDir()

	freshObs := now.Add(-2 * time.Minute).Format(time.RFC3339)
	staleObs := now.Add(-2 * time.Hour).Format(time.RFC3339)
	futureReset := now.Add(3 * time.Hour).Format(time.RFC3339)

	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"policy":{"min_remaining_pct":10}}`), 0o600)
	accounts := map[string]any{
		"active@example.com": map[string]any{
			"email": "active@example.com", "name": "Active", "secret_ref": "v1",
		},
		"fresh@example.com": map[string]any{
			"email": "fresh@example.com", "name": "Fresh Pro", "secret_ref": "v2",
			"quota_snapshot": map[string]any{
				"observed_at": freshObs,
				"groups": []map[string]any{{
					"id": "gemini", "buckets": []map[string]any{
						{"id": "gemini-5h", "remaining_fraction": 1.0, "reset_at": futureReset, "window": "5h"},
					},
				}},
			},
		},
		"stale@example.com": map[string]any{
			"email": "stale@example.com", "name": "Stale Pro", "secret_ref": "v3",
			"quota_snapshot": map[string]any{
				"observed_at": staleObs,
				"groups": []map[string]any{{
					"id": "gemini", "buckets": []map[string]any{
						{"id": "gemini-5h", "remaining_fraction": 0.85, "reset_at": futureReset, "window": "5h"},
					},
				}},
			},
		},
	}
	b, _ := json.Marshal(accounts)
	_ = os.WriteFile(filepath.Join(dir, "accounts.json"), b, 0o600)

	env := envMap(map[string]string{
		"AGY_SWAP_DIR":             dir,
		"AGY_STATUSLINE_STATE_DIR": stateDir,
		"NO_COLOR":                 "1",
	})
	data := &StatusLineData{
		Email:         "active@example.com",
		TerminalWidth: 142,
		Model:         &ModelInfo{ID: "gemini-pro-orchestrator"},
		ContextWindow: &ContextInfo{ContextWindowSize: 200_000, UsedPercentage: f64Ptr(10)},
		Quota: map[string]QuotaBucket{
			"gemini-5h": {RemainingFraction: f64Ptr(0.80), ResetInSeconds: i64Ptr(3600)},
		},
	}
	rendered := Render(data, now, env)
	lines := strings.Split(rendered, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), rendered)
	}
	if DisplayWidth(lines[1]) != 142 {
		t.Fatalf("row 2 width mismatch: got %d want 142 (%q)", DisplayWidth(lines[1]), lines[1])
	}
	if !strings.Contains(lines[1], "~85%") {
		t.Fatalf("expected stale indicator ~85%% on stale reserve, got %q", lines[1])
	}
	if strings.Contains(lines[1], "~100%") {
		t.Fatalf("fresh reserve should not carry ~ stale marker, got %q", lines[1])
	}
}

func TestSessionIsolationWriteOnChangeAndTempCleanup(t *testing.T) {
	t0 := time.Date(2026, 11, 19, 15, 0, 0, 0, time.UTC)
	stateDir := t.TempDir()
	env := envMap(map[string]string{
		"AGY_STATUSLINE_STATE_DIR": stateDir,
		"AGY_SWAP_DIR":             filepath.Join(stateDir, "missing-swap"),
		"NO_COLOR":                 "1",
	})

	// Seed an old orphaned legacy tmp-* file and a fresh .tmp. file.
	oldTmp := filepath.Join(stateDir, "tmp-9999999")
	freshTmp := filepath.Join(stateDir, "fleet.json.tmp.99999.1")
	_ = os.WriteFile(oldTmp, []byte(""), 0o600)
	_ = os.WriteFile(freshTmp, []byte(""), 0o600)
	past := time.Now().Add(-5 * time.Minute)
	_ = os.Chtimes(oldTmp, past, past)

	dA := &StatusLineData{
		ConversationID: "conv-a",
		Email:          "alex@example.com",
		TerminalWidth:  120,
		Model:          &ModelInfo{ID: "gemini-pro-orchestrator"},
		ContextWindow:  &ContextInfo{ContextWindowSize: 200_000, CurrentUsage: &CurrentUsageInfo{InputTokens: 10_000}},
		Quota:          map[string]QuotaBucket{"gemini-5h": {RemainingFraction: f64Ptr(0.5), ResetInSeconds: i64Ptr(3600)}},
	}
	dB := &StatusLineData{
		ConversationID: "conv-b",
		Email:          "alex@example.com",
		TerminalWidth:  120,
		Model:          &ModelInfo{ID: "gemini-pro-orchestrator"},
		ContextWindow:  &ContextInfo{ContextWindowSize: 200_000, CurrentUsage: &CurrentUsageInfo{InputTokens: 25_000}},
		Quota:          map[string]QuotaBucket{"gemini-5h": {RemainingFraction: f64Ptr(0.5), ResetInSeconds: i64Ptr(3600)}},
	}

	_ = Render(dA, t0, env)
	_ = Render(dB, t0, env)

	// Old orphaned temp must be swept while fresh temp remains untouched.
	if _, err := os.Stat(oldTmp); !os.IsNotExist(err) {
		t.Fatalf("expected old orphaned tmp file to be swept, err=%v", err)
	}
	if _, err := os.Stat(freshTmp); err != nil {
		t.Fatalf("fresh tmp file should not be removed prematurely: %v", err)
	}

	sessAPath := filepath.Join(stateDir, "sessions", shortHash("conv-a")+".json")
	sessBPath := filepath.Join(stateDir, "sessions", shortHash("conv-b")+".json")
	fleetPath := filepath.Join(stateDir, "fleet.json")

	stA1, err := os.Stat(sessAPath)
	if err != nil {
		t.Fatalf("missing per-session file for conv-a: %v", err)
	}
	if _, err := os.Stat(sessBPath); err != nil {
		t.Fatalf("missing per-session file for conv-b: %v", err)
	}
	stF1, err := os.Stat(fleetPath)
	if err != nil {
		t.Fatalf("missing fleet.json: %v", err)
	}

	// Render again 2 seconds later with unchanged tokens & quota -> ZERO disk rewrites.
	time.Sleep(15 * time.Millisecond)
	_ = Render(dA, t0.Add(2*time.Second), env)

	stA2, _ := os.Stat(sessAPath)
	stF2, _ := os.Stat(fleetPath)
	if !stA2.ModTime().Equal(stA1.ModTime()) {
		t.Fatalf("expected unchanged session render to skip disk write")
	}
	if !stF2.ModTime().Equal(stF1.ModTime()) {
		t.Fatalf("expected unchanged quota render to skip fleet.json rewrite")
	}
}

func TestGitFingerprintInvalidationAndSWR(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()
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

	var asyncTriggered []string
	origRefresher := gitAsyncRefresher
	gitAsyncRefresher = func(sd, d string) {
		asyncTriggered = append(asyncTriggered, d)
	}
	defer func() { gitAsyncRefresher = origRefresher }()

	t0 := time.Date(2026, 11, 19, 15, 0, 0, 0, time.UTC)
	data := &StatusLineData{
		Workspace: &WorkspaceInfo{CurrentDir: repo, ProjectDir: repo},
	}

	// Cold probe caches "main".
	g1 := loadGitStatus(data, stateDir, t0, nil)
	if !g1.Probed || g1.Branch != "main" {
		t.Fatalf("expected initial probe on main, got %+v", g1)
	}
	if len(asyncTriggered) != 0 {
		t.Fatalf("cold probe succeeded synchronously; expected 0 async calls, got %d", len(asyncTriggered))
	}

	// Fresh cache hit within 2s TTL and matching fingerprint -> 0 async calls.
	g2 := loadGitStatus(data, stateDir, t0.Add(500*time.Millisecond), nil)
	if g2.Branch != "main" || len(asyncTriggered) != 0 {
		t.Fatalf("expected fresh cache hit, got detail=%+v async=%d", g2, len(asyncTriggered))
	}

	// Switch branch -> HEAD fingerprint changes immediately (still well within 2s TTL!).
	runGit("checkout", "-b", "feat/realtime")
	g3 := loadGitStatus(data, stateDir, t0.Add(600*time.Millisecond), nil)
	if g3.Branch != "feat/realtime" {
		t.Fatalf("expected instant HEAD branch update on frame 0, got %+v", g3)
	}
	if len(asyncTriggered) != 1 {
		t.Fatalf("expected 1 background SWR refresh trigger on fingerprint mismatch, got %d", len(asyncTriggered))
	}

	// Execute locked refresh helper -> subsequent call is fresh with 0 new triggers.
	if ok := RefreshGitCacheLocked(stateDir, repo, t0.Add(700*time.Millisecond)); !ok {
		t.Fatal("RefreshGitCacheLocked failed")
	}
	g4 := loadGitStatus(data, stateDir, t0.Add(800*time.Millisecond), nil)
	if g4.Branch != "feat/realtime" || len(asyncTriggered) != 1 {
		t.Fatalf("expected fresh cache after RefreshGitCacheLocked, got %+v async=%d", g4, len(asyncTriggered))
	}

	// TTL expiry (>2s) returns cached immediately and queues async refresh without blocking.
	g5 := loadGitStatus(data, stateDir, t0.Add(5*time.Second), nil)
	if g5.Branch != "feat/realtime" || len(asyncTriggered) != 2 {
		t.Fatalf("expected SWR trigger after TTL expiry, got %+v async=%d", g5, len(asyncTriggered))
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
