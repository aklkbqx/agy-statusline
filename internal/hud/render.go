package hud

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxStdinBytes = 256 * 1024

var (
	brailleFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	eqFrames      = []string{"▁▃▅", "▃▅▇", "▅▇▃", "▇▃▁"}
)

// RenderReader parses StatusLineData JSON from r and returns the formatted ANSI HUD.
func RenderReader(r io.Reader, now time.Time, getenv func(string) string, opts ...RenderOptions) string {
	raw, err := io.ReadAll(io.LimitReader(r, maxStdinBytes))
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	var data StatusLineData
	if err := json.Unmarshal(raw, &data); err != nil {
		return ""
	}
	return Render(&data, now, getenv, opts...)
}

// Render builds the responsive 1-row or 2-row Cockpit HUD from data and environment config.
func Render(data *StatusLineData, now time.Time, getenv func(string) string, opts ...RenderOptions) string {
	if data == nil {
		return ""
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	var opt RenderOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	family := ActiveFamily(data.Model)
	stateDir := ResolveStateDir(getenv)
	fleetCache := loadAndUpdateFleetCache(stateDir, data, now)
	swapInfo := LoadAgySwapInfo(data, family, fleetCache, now, getenv)

	quota := data.Quota
	if len(quota) == 0 && len(swapInfo.FallbackQuota) > 0 {
		quota = swapInfo.FallbackQuota
	}

	usedTokens, ctxPct, ctxMax, cachePct := computeContextStats(data.ContextWindow, data.Exceeds200kTokens)
	sess := loadAndUpdateSessionState(stateDir, data, usedTokens, now)
	git := loadGitStatus(data, stateDir, now, getenv)

	return renderWithResolvedState(data, quota, swapInfo, git, usedTokens, ctxPct, ctxMax, cachePct, sess, family, now, getenv, opt)
}

// RenderDemo produces a synthetic preview with zero disk state writes.
func RenderDemo(width int, agentState string, themeName string, singleLine bool, now time.Time, getenv func(string) string) string {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	f := func(v float64) *float64 { return &v }
	i := func(v int64) *int64 { return &v }

	needsConfirm := strings.EqualFold(strings.TrimSpace(agentState), "confirm")
	effState := agentState
	if needsConfirm {
		effState = "idle"
	}

	data := &StatusLineData{
		ConversationID:          "demo",
		ConversationTitle:       "Cockpit HUD",
		AgentState:              effState,
		ToolConfirmationPending: needsConfirm,
		Model:                   &ModelInfo{ID: "gemini-pro-orchestrator", DisplayName: "Orchestrator (Carbon)"},
		Workspace:               &WorkspaceInfo{CurrentDir: "/workspace/agy-statusline", ProjectDir: "/workspace/agy-statusline"},
		ContextWindow: &ContextInfo{
			ContextWindowSize: 1_000_000,
			UsedPercentage:    f(28),
			CurrentUsage:      &CurrentUsageInfo{InputTokens: 40_000, CacheReadInputTokens: 214_000},
		},
		Quota: map[string]QuotaBucket{
			"gemini-5h":     {RemainingFraction: f(0.09), ResetInSeconds: i(2*3600 + 4*60)},
			"gemini-weekly": {RemainingFraction: f(0.47), ResetInSeconds: i(6*86400 + 11*3600)},
			"3p-5h":         {RemainingFraction: f(1.0), ResetInSeconds: i(5 * 3600)},
			"3p-weekly":     {RemainingFraction: f(1.0), ResetInSeconds: i(7 * 86400)},
		},
		Email:         "alex@example.com",
		PlanTier:      "ULTRA",
		TerminalWidth: width,
		Subagents:     []SubagentInfo{{Name: "reviewer", Status: "running"}},
		TaskCount:     1,
		Cost:          &CostInfo{TotalUSD: 0.42},
	}

	swapInfo := AgySwapInfo{
		Alias:         "Studio Ultra",
		ActiveTier:    "ULTRA",
		ReadyReserves: 3,
		TotalAccounts: 5,
		Fleet: []FleetAccountView{
			{Email: "sam@example.com", Alias: "Dev Pro", Tier: "PRO", Status: "ready", PrimaryPct: 100},
			{Email: "riley@example.com", Alias: "Ship Pro", Tier: "PRO", Status: "ready", PrimaryPct: 100},
			{Email: "morgan@example.com", Alias: "Build Pro", Tier: "PRO", Status: "ready", PrimaryPct: 95},
			{Email: "jordan@example.com", Alias: "Lab Ultra", Tier: "ULTRA", Status: "partial", PrimaryPct: 100, SecondaryPct: 100, HasSecondary: true, WaitSeconds: 2*86400 + 21*3600},
		},
	}

	git := GitDetail{
		Branch:   "main",
		Staged:   1,
		Modified: 2,
		Ahead:    1,
		Probed:   true,
	}
	sess := SessionView{
		ElapsedSec:        14 * 60,
		ActiveDeltaTokens: 14_000,
	}
	usedTokens, ctxPct, ctxMax, cachePct := computeContextStats(data.ContextWindow, nil)
	return renderWithResolvedState(
		data,
		data.Quota,
		swapInfo,
		git,
		usedTokens,
		ctxPct,
		ctxMax,
		cachePct,
		sess,
		"gemini",
		now,
		getenv,
		RenderOptions{SingleLine: singleLine, ThemeName: themeName},
	)
}

func renderWithResolvedState(
	data *StatusLineData,
	quota map[string]QuotaBucket,
	swapInfo AgySwapInfo,
	git GitDetail,
	usedTokens int,
	ctxPct float64,
	ctxMax int,
	cachePct float64,
	sess SessionView,
	family string,
	now time.Time,
	getenv func(string) string,
	opt RenderOptions,
) string {
	noColor := getenv("NO_COLOR") != "" || strings.EqualFold(getenv("TERM"), "dumb")
	noBlink := isTruthy(getenv("AGY_STATUSLINE_NO_BLINK"))
	themeName := ResolveThemeName(opt.ThemeName, getenv)
	th := newTheme(themeName, !noColor, noBlink)

	hideAccount := isTruthy(getenv("AGY_CLI_HIDE_ACCOUNT_INFO"))
	singleLine := opt.SingleLine || isTruthy(getenv("AGY_STATUSLINE_SINGLE_LINE"))
	cols := resolveWidth(data.TerminalWidth, getenv)
	frame := now.UnixMilli() / 240

	showRow2 := !singleLine && cols >= 58 && !hideAccount && len(swapInfo.Fleet) > 0

	row1 := renderRow1(data, quota, swapInfo, git, usedTokens, ctxPct, ctxMax, cachePct, sess, family, hideAccount, showRow2, cols, frame, now, getenv, th)
	if !showRow2 {
		return row1
	}

	row2 := renderRow2(quota, swapInfo, family, cols, frame, th)
	if row2 == "" {
		return row1
	}
	return row1 + "\n" + row2
}

func resolveWidth(payloadWidth int, getenv func(string) string) int {
	if payloadWidth > 0 {
		return payloadWidth
	}
	if getenv != nil {
		if c := strings.TrimSpace(getenv("COLUMNS")); c != "" {
			if n, err := strconv.Atoi(c); err == nil && n > 0 {
				return n
			}
		}
	}
	return 100
}

// ── Row 1: Primary Session & Telemetry Builder ─────────────────────────────

func isWorkingState(agentState string) bool {
	s := strings.ToLower(strings.TrimSpace(agentState))
	switch s {
	case "", "idle", "ready", "done", "completed", "waiting", "stopped":
		return false
	default:
		return true
	}
}

func formatWorkingLabel(agentState string) string {
	s := strings.ToLower(strings.TrimSpace(agentState))
	switch s {
	case "thinking":
		return "THINKING"
	case "tool_use", "tool", "executing":
		return "TOOL"
	case "streaming", "generating":
		return "STREAM"
	case "retrying":
		return "RETRY"
	case "initializing":
		return "INIT"
	case "working", "running":
		return "WORKING"
	default:
		return clip(strings.ToUpper(s), 8)
	}
}

func renderRow1(
	data *StatusLineData,
	quota map[string]QuotaBucket,
	swapInfo AgySwapInfo,
	git GitDetail,
	usedTokens int,
	ctxPct float64,
	ctxMax int,
	cachePct float64,
	sess SessionView,
	family string,
	hideAccount bool,
	showRow2 bool,
	cols int,
	frame int64,
	now time.Time,
	getenv func(string) string,
	th theme,
) string {
	working := isWorkingState(data.AgentState)
	needsAction := data.ToolConfirmationPending || data.PendingInputCount > 0

	startLevel := preferredRow1Level(cols)
	for level := startLevel; level >= 0; level-- {
		leftGroups := buildRow1LeftGroups(data, quota, swapInfo, usedTokens, ctxPct, ctxMax, cachePct, sess, family, hideAccount, showRow2, working, needsAction, level, frame, now, th)
		leftW, leftStr := renderGroups(leftGroups, th)
		if leftW > cols && level > 0 {
			continue
		}

		candidates := buildRow1RightCandidates(data, git, sess, level, frame, getenv, th)
		if level >= 2 && len(candidates) > 0 {
			minRight := spansWidth(candidates[0].spans)
			if leftW+2+minRight > cols {
				continue
			}
		}

		if leftW > cols {
			return hardTruncateANSI(leftStr, cols, th.enabled)
		}

		rightW, rightStr := packRightCandidates(candidates, cols-leftW-2, th)
		if rightW > 0 {
			gap := cols - leftW - rightW
			if gap < 2 {
				gap = 2
			}
			return leftStr + strings.Repeat(" ", gap) + rightStr
		}
		return leftStr
	}

	leftGroups := buildRow1LeftGroups(data, quota, swapInfo, usedTokens, ctxPct, ctxMax, cachePct, sess, family, hideAccount, showRow2, working, needsAction, 0, frame, now, th)
	_, leftStr := renderGroups(leftGroups, th)
	return hardTruncateANSI(leftStr, cols, th.enabled)
}

func preferredRow1Level(cols int) int {
	switch {
	case cols >= 144:
		return 5
	case cols >= 118:
		return 4
	case cols >= 92:
		return 3
	case cols >= 68:
		return 2
	case cols >= 48:
		return 1
	default:
		return 0
	}
}

func buildRow1LeftGroups(
	data *StatusLineData,
	quota map[string]QuotaBucket,
	swapInfo AgySwapInfo,
	usedTokens int,
	ctxPct float64,
	ctxMax int,
	cachePct float64,
	sess sessionStateOrView,
	family string,
	hideAccount bool,
	showRow2 bool,
	working bool,
	needsAction bool,
	level int,
	frame int64,
	now time.Time,
	th theme,
) []group {
	var groups []group

	primary5h, has5h := lookupBucket(quota, family, "5h")
	primary7d, has7d := lookupBucket(quota, family, "weekly")
	primaryMinPct, hasPrimaryMin := poolMinRemainingPct(primary5h, has5h, primary7d, has7d)
	lowQuota := hasPrimaryMin && primaryMinPct < 25.0

	if acct := buildRow1IdentityGroup(data, swapInfo, lowQuota, hideAccount, showRow2, working, needsAction, level, frame, th); len(acct) > 0 {
		groups = append(groups, acct)
	}

	if qg := buildRow1QuotaGroup(quota, family, primary5h, has5h, primary7d, has7d, working, level, frame, now, th); len(qg) > 0 {
		groups = append(groups, qg)
	}

	if cg := buildRow1ContextGroup(usedTokens, ctxPct, ctxMax, cachePct, sess.ActiveDeltaTokens, working, level, frame, th); len(cg) > 0 {
		groups = append(groups, cg)
	}

	return groups
}

type sessionStateOrView = SessionView

func buildRow1IdentityGroup(
	data *StatusLineData,
	swapInfo AgySwapInfo,
	lowQuota bool,
	hideAccount bool,
	showRow2 bool,
	working bool,
	needsAction bool,
	level int,
	frame int64,
	th theme,
) group {
	if hideAccount && !working && !needsAction {
		return nil
	}

	alias := ""
	if !hideAccount {
		maxA := 12
		if level >= 4 {
			maxA = 14
		} else if level <= 1 {
			maxA = 8
		}
		alias = shortAlias(swapInfo.Alias, maxA)
	}

	showInlineSwap := !hideAccount && lowQuota && (!showRow2 || len(swapInfo.Fleet) == 0)

	if level == 0 {
		if needsAction {
			return group{{text: "◉ ACTION", style: th.style(th.p.Amber, nil, true, true)}}
		}
		if working {
			spin := brailleFrames[int(frame%int64(len(brailleFrames)))]
			return group{{text: spin, style: th.fg(th.p.Cyan)}}
		}
		if showInlineSwap {
			return inlineSwapSpan(swapInfo.ReadyReserves, false, th)
		}
		return nil
	}

	if level == 1 {
		var g group
		if needsAction {
			g = append(g,
				span{text: "◉", style: th.style(th.p.Amber, nil, true, true)},
				span{text: " ACT", style: th.fgBold(th.p.Amber)},
			)
		} else if working {
			spin := brailleFrames[int(frame%int64(len(brailleFrames)))]
			g = append(g, span{text: spin, style: th.fg(th.p.Cyan)})
			if alias != "" {
				g = append(g, span{text: " " + alias, style: th.fg(th.p.Text)})
			}
		} else if alias != "" {
			g = append(g,
				span{text: "⬢", style: th.fg(th.p.Orange)},
				span{text: alias, style: th.fg(th.p.Text)},
			)
		}
		if showInlineSwap {
			g = append(g, inlineSwapSpan(swapInfo.ReadyReserves, len(g) > 0, th)...)
		}
		return g
	}

	var g group
	var bg rgb
	switch {
	case needsAction:
		bg = th.p.BgAlertPill
		g = append(g,
			span{text: " ", style: th.bgOnly(bg)},
			span{text: "◉", style: th.style(th.p.Amber, &bg, true, true)},
		)
		if alias != "" {
			g = append(g, span{text: " " + alias, style: th.style(th.p.Text, &bg, true, false)})
		}
		g = append(g,
			span{text: " · ", style: th.style(th.p.Muted, &bg, false, false)},
			span{text: "CONFIRM", style: th.style(th.p.Amber, &bg, true, false)},
			span{text: " ", style: th.bgOnly(bg)},
		)
	case working:
		bg = th.p.BgWorkPill
		spin := brailleFrames[int(frame%int64(len(brailleFrames)))]
		g = append(g,
			span{text: " ", style: th.bgOnly(bg)},
			span{text: spin, style: th.style(th.p.Cyan, &bg, true, false)},
		)
		if alias != "" {
			g = append(g,
				span{text: " " + alias, style: th.style(th.p.Text, &bg, true, false)},
				span{text: " · ", style: th.style(th.p.Dim, &bg, false, false)},
			)
		} else {
			g = append(g, span{text: " ", style: th.bgOnly(bg)})
		}
		g = append(g, shimmerSpans(formatWorkingLabel(data.AgentState), bg, frame, th)...)
		g = append(g, span{text: " ", style: th.bgOnly(bg)})
	default:
		if alias == "" {
			return nil
		}
		bg = th.p.BgActivePill
		g = append(g,
			span{text: " ", style: th.bgOnly(bg)},
			span{text: "⬢ ", style: th.style(th.p.Orange, &bg, false, false)},
			span{text: alias, style: th.style(th.p.Text, &bg, true, false)},
		)
		if level >= 4 && swapInfo.ActiveTier != "" {
			tcol := th.p.Cyan
			if swapInfo.ActiveTier == "ULTRA" {
				tcol = th.p.Purple
			}
			g = append(g,
				span{text: " · ", style: th.style(th.p.Dim, &bg, false, false)},
				span{text: swapInfo.ActiveTier, style: th.style(tcol, &bg, true, false)},
			)
		}
		g = append(g, span{text: " ", style: th.bgOnly(bg)})
	}

	if showInlineSwap {
		g = append(g, inlineSwapSpan(swapInfo.ReadyReserves, true, th)...)
	}
	return g
}

func inlineSwapSpan(readyReserves int, leadingSpace bool, th theme) group {
	prefix := ""
	if leadingSpace {
		prefix = " "
	}
	if readyReserves > 0 {
		return group{{text: fmt.Sprintf("%s⇄%d", prefix, readyReserves), style: th.style(th.p.Green, nil, true, true)}}
	}
	return group{{text: fmt.Sprintf("%s!0", prefix), style: th.style(th.p.Red, nil, true, true)}}
}

func buildRow1QuotaGroup(
	quota map[string]QuotaBucket,
	family string,
	b5h QuotaBucket,
	has5h bool,
	b7d QuotaBucket,
	has7d bool,
	working bool,
	level int,
	frame int64,
	now time.Time,
	th theme,
) group {
	if !has5h && !has7d {
		return nil
	}

	priLabel := "GEM"
	if level <= 2 {
		priLabel = "Gem"
	}
	secFam, secLabel := secondaryFamily(family)
	if family == "3p" {
		priLabel = "3P"
	}

	var g group
	if level == 0 {
		if has5h {
			pct5h := bucketRemainingPct(b5h)
			c, pace := quotaColorRGB(pct5h, b5h, 18000, now, th)
			g = append(g, span{text: "5h ", style: th.fg(th.p.Muted)})
			if pct5h <= 15.0 {
				g = append(g, span{text: "●", style: th.style(th.p.Red, nil, true, true)})
			}
			g = append(g, span{text: fmt.Sprintf("%.0f%%", math.Round(pct5h)), style: th.fgBold(c)})
			if pace {
				g = append(g, span{text: "▲", style: th.fg(th.p.Amber)})
			}
			if remSec, ok := bucketResetSeconds(b5h, now); ok && remSec > 0 && pct5h <= 60.0 {
				g = append(g, span{text: "↻" + formatCompactSpan(remSec), style: th.fg(th.p.Dim)})
			}
		} else {
			pct7d := bucketRemainingPct(b7d)
			c, _ := quotaColorRGB(pct7d, b7d, 604800, now, th)
			g = append(g,
				span{text: "7d ", style: th.fg(th.p.Muted)},
				span{text: fmt.Sprintf("%.0f%%", math.Round(pct7d)), style: th.fgBold(c)},
			)
		}
		return g
	}

	if level == 1 {
		if has5h {
			pct5h := bucketRemainingPct(b5h)
			c, pace := quotaColorRGB(pct5h, b5h, 18000, now, th)
			g = append(g, span{text: "5h ", style: th.fg(th.p.Dim)})
			if pct5h <= 15.0 {
				g = append(g, span{text: "●", style: th.style(th.p.Red, nil, true, true)})
			}
			g = append(g, span{text: fmt.Sprintf("%.0f%%", math.Round(pct5h)), style: th.style(c, nil, pct5h <= 20, false)})
			if pace {
				g = append(g, span{text: "▲", style: th.fg(th.p.Amber)})
			}
			if remSec, ok := bucketResetSeconds(b5h, now); ok && remSec > 0 && pct5h < 99.5 {
				g = append(g, span{text: " ↻" + formatCompactSpan(remSec), style: th.fg(th.p.Dim)})
			}
		}
		if has7d {
			if len(g) > 0 {
				g = append(g, span{text: " · ", style: th.fg(th.p.Dim)})
			}
			pct7d := bucketRemainingPct(b7d)
			c, pace := quotaColorRGB(pct7d, b7d, 604800, now, th)
			g = append(g,
				span{text: "7d ", style: th.fg(th.p.Dim)},
				span{text: fmt.Sprintf("%.0f%%", math.Round(pct7d)), style: th.fg(c)},
			)
			if pace {
				g = append(g, span{text: "▲", style: th.fg(th.p.Amber)})
			}
		}
		return g
	}

	bar5Cells := 4
	bar7Cells := 0
	barSecCells := 0
	switch level {
	case 5:
		bar5Cells, bar7Cells, barSecCells = 8, 5, 4
	case 4:
		bar5Cells, bar7Cells, barSecCells = 6, 4, 0
	case 3:
		bar5Cells, bar7Cells, barSecCells = 5, 0, 0
	default:
		bar5Cells, bar7Cells, barSecCells = 4, 0, 0
	}

	g = append(g, span{text: priLabel + " ", style: th.fg(th.p.Muted)})
	firstAdded := false
	if has5h {
		pct5h := bucketRemainingPct(b5h)
		c, pace := quotaColorRGB(pct5h, b5h, 18000, now, th)
		g = append(g, span{text: "5h ", style: th.fg(th.p.Dim)})
		g = append(g, renderLaserBar(pct5h, bar5Cells, c, working, frame, th)...)
		g = append(g, span{text: " ", style: ""})
		if pct5h <= 15.0 {
			g = append(g, span{text: "●", style: th.style(th.p.Red, nil, true, true)})
		}
		g = append(g, span{text: fmt.Sprintf("%.0f%%", math.Round(pct5h)), style: th.style(c, nil, pct5h <= 20, false)})
		if pace {
			g = append(g, span{text: "▲", style: th.fg(th.p.Amber)})
		}
		if remSec, ok := bucketResetSeconds(b5h, now); ok && remSec > 0 && pct5h < 99.5 {
			g = append(g, span{text: " ↻" + formatCompactSpan(remSec), style: th.fg(th.p.Dim)})
		}
		firstAdded = true
	}

	if has7d {
		if firstAdded {
			g = append(g, span{text: " · ", style: th.fg(th.p.Dim)})
		}
		pct7d := bucketRemainingPct(b7d)
		c, pace := quotaColorRGB(pct7d, b7d, 604800, now, th)
		g = append(g, span{text: "7d ", style: th.fg(th.p.Dim)})
		if bar7Cells > 0 {
			g = append(g, renderLaserBar(pct7d, bar7Cells, c, working, frame, th)...)
			g = append(g, span{text: " ", style: ""})
		}
		g = append(g, span{text: fmt.Sprintf("%.0f%%", math.Round(pct7d)), style: th.style(c, nil, pct7d <= 20, false)})
		if pace {
			g = append(g, span{text: "▲", style: th.fg(th.p.Amber)})
		}
		if remSec, ok := bucketResetSeconds(b7d, now); ok && remSec > 0 && pct7d <= 30.0 {
			g = append(g, span{text: " ↻" + formatCompactSpan(remSec), style: th.fg(th.p.Dim)})
		}
	}

	if level >= 3 {
		sec5h, hasSec5h := lookupBucket(quota, secFam, "5h")
		sec7d, hasSec7d := lookupBucket(quota, secFam, "weekly")
		if secMin, ok := poolMinRemainingPct(sec5h, hasSec5h, sec7d, hasSec7d); ok {
			secCol := th.p.Muted
			if secMin < 50.0 {
				var ref QuotaBucket
				var win int64 = 18000
				if hasSec5h && (!hasSec7d || bucketRemainingPct(sec5h) <= bucketRemainingPct(sec7d)) {
					ref = sec5h
					win = 18000
				} else {
					ref = sec7d
					win = 604800
				}
				secCol, _ = quotaColorRGB(secMin, ref, win, now, th)
			}
			g = append(g,
				span{text: " · ", style: th.fg(th.p.Dim)},
				span{text: secLabel + " ", style: th.fg(th.p.Dim)},
			)
			if barSecCells > 0 {
				barCol := th.p.Green
				if secMin < 50.0 {
					barCol = secCol
				}
				g = append(g, renderLaserBar(secMin, barSecCells, barCol, false, frame, th)...)
				g = append(g, span{text: " ", style: ""})
			}
			g = append(g, span{text: fmt.Sprintf("%.0f%%", math.Round(secMin)), style: th.fg(secCol)})
		}
	}
	return g
}

func computeContextStats(ctx *ContextInfo, exceeds200k *bool) (usedTokens int, pct float64, windowSize int, cachePct float64) {
	if ctx == nil {
		return 0, 0, 0, 0
	}
	windowSize = ctx.ContextWindowSize
	if ctx.CurrentUsage != nil {
		cu := ctx.CurrentUsage
		usedTokens = cu.InputTokens + cu.CacheCreationInputTokens + cu.CacheReadInputTokens
		if usedTokens >= 2000 && cu.CacheReadInputTokens > 0 {
			cachePct = clamp(float64(cu.CacheReadInputTokens)/float64(usedTokens)*100.0, 0, 100)
		}
	}
	if ctx.UsedPercentage != nil {
		pct = clamp(*ctx.UsedPercentage, 0, 100)
	} else if windowSize > 0 && usedTokens > 0 {
		pct = clamp(float64(usedTokens)/float64(windowSize)*100.0, 0, 100)
	}
	if usedTokens == 0 && pct > 0 && windowSize > 0 {
		usedTokens = int(math.Round(pct / 100.0 * float64(windowSize)))
	}
	allowExceed := exceeds200k != nil && *exceeds200k
	if windowSize > 0 && !allowExceed && usedTokens > windowSize {
		usedTokens = windowSize
	}
	return usedTokens, pct, windowSize, cachePct
}

func buildRow1ContextGroup(
	usedTokens int,
	pct float64,
	windowSize int,
	cachePct float64,
	activeDelta int,
	working bool,
	level int,
	frame int64,
	th theme,
) group {
	c := ctxColorRGB(pct, th)
	var g group

	if level <= 1 {
		g = append(g,
			span{text: renderPie(pct), style: th.fg(c)},
			span{text: fmt.Sprintf(" %.0f%%", math.Round(pct)), style: th.style(c, nil, pct >= 80, false)},
		)
		return g
	}

	cells := 4
	switch level {
	case 5:
		cells = 8
	case 4:
		cells = 6
	case 3:
		cells = 5
	}

	g = append(g, span{text: "CTX ", style: th.fg(th.p.Muted)})
	g = append(g, renderLaserBar(pct, cells, c, working, frame, th)...)
	g = append(g, span{text: fmt.Sprintf(" %.0f%%", math.Round(pct)), style: th.style(c, nil, pct >= 80, false)})

	if level >= 3 && windowSize > 0 {
		g = append(g, span{
			text:  fmt.Sprintf(" %s/%s", formatTokens(usedTokens), formatTokens(windowSize)),
			style: th.fg(th.p.Dim),
		})
	}
	if level >= 4 && activeDelta >= 500 {
		g = append(g, span{
			text:  fmt.Sprintf(" +%s", formatTokens(activeDelta)),
			style: th.fg(th.p.Cyan),
		})
	}
	if level >= 4 && cachePct >= 10.0 {
		g = append(g, span{
			text:  fmt.Sprintf(" ⌁%.0f%%", math.Round(cachePct)),
			style: th.fg(th.p.Green),
		})
	}
	return g
}

type rightCandidate struct {
	priority int
	order    int
	spans    group
}

func buildRow1RightCandidates(
	data *StatusLineData,
	git GitDetail,
	sess SessionView,
	level int,
	frame int64,
	getenv func(string) string,
	th theme,
) []rightCandidate {
	var out []rightCandidate

	if gg := buildGitRightGroup(git, level, th); len(gg) > 0 {
		out = append(out, rightCandidate{priority: 1, order: 3, spans: gg})
	}

	if level >= 2 {
		if ws := workspaceLabel(data, level, getenv); ws != "" {
			out = append(out, rightCandidate{
				priority: 2,
				order:    2,
				spans: group{
					{text: "⌂ ", style: th.fg(th.p.Dim)},
					{text: ws, style: th.fg(th.p.Cyan)},
				},
			})
		}
	}

	if act := buildActivityRightGroup(data, level, frame, th); len(act) > 0 {
		out = append(out, rightCandidate{priority: 3, order: 4, spans: act})
	}

	if level >= 2 && data.Cost != nil && data.Cost.TotalUSD >= 0.005 {
		out = append(out, rightCandidate{
			priority: 4,
			order:    5,
			spans:    group{{text: formatMoney(data.Cost.TotalUSD), style: th.fg(th.p.Muted)}},
		})
	}

	if level >= 3 && sess.ElapsedSec >= 60 {
		out = append(out, rightCandidate{
			priority: 5,
			order:    6,
			spans:    group{{text: "◴" + formatCompactSpan(sess.ElapsedSec), style: th.fg(th.p.Dim)}},
		})
	}

	if level >= 4 && strings.TrimSpace(data.ConversationTitle) != "" {
		maxT := 16
		if level >= 5 {
			maxT = 20
		}
		out = append(out, rightCandidate{
			priority: 6,
			order:    1,
			spans:    group{{text: "‹" + clip(strings.TrimSpace(data.ConversationTitle), maxT) + "›", style: th.fg(th.p.Muted)}},
		})
	}

	return out
}

func buildGitRightGroup(git GitDetail, level int, th theme) group {
	if git.Branch == "" {
		return nil
	}
	var g group
	if level >= 2 && git.IsWorktree {
		g = append(g, span{text: "⧉ ", style: th.fg(th.p.Muted)})
	}
	maxBranch := 10
	switch {
	case level >= 4:
		maxBranch = 22
	case level >= 2:
		maxBranch = 15
	}
	g = append(g,
		span{text: "⎇ ", style: th.fg(th.p.Dim)},
		span{text: clip(git.Branch, maxBranch), style: th.fg(th.p.Purple)},
	)

	if level >= 4 {
		if git.Conflicts > 0 {
			g = append(g, span{text: fmt.Sprintf(" ✖%d", git.Conflicts), style: th.fgBold(th.p.Red)})
		}
		if git.Staged > 0 {
			g = append(g, span{text: fmt.Sprintf(" +%d", git.Staged), style: th.fg(th.p.Green)})
		}
		if git.Modified > 0 {
			g = append(g, span{text: fmt.Sprintf(" ~%d", git.Modified), style: th.fg(th.p.Amber)})
		}
		if level >= 5 && git.Untracked > 0 {
			g = append(g, span{text: fmt.Sprintf(" ?%d", git.Untracked), style: th.fg(th.p.Dim)})
		}
		if !git.Probed && git.Dirty && git.Conflicts+git.Staged+git.Modified+git.Untracked == 0 {
			g = append(g, span{text: " ●", style: th.fg(th.p.Amber)})
		}
		if git.Ahead > 0 {
			g = append(g, span{text: fmt.Sprintf(" ↑%d", git.Ahead), style: th.fg(th.p.Cyan)})
		}
		if git.Behind > 0 {
			g = append(g, span{text: fmt.Sprintf(" ↓%d", git.Behind), style: th.fg(th.p.Amber)})
		}
	} else if level >= 2 {
		if git.Conflicts > 0 {
			g = append(g, span{text: fmt.Sprintf(" ✖%d", git.Conflicts), style: th.fgBold(th.p.Red)})
		} else if git.Staged+git.Modified+git.Untracked > 0 || git.Dirty {
			g = append(g, span{text: " ●", style: th.fg(th.p.Amber)})
		}
		if git.Ahead > 0 {
			g = append(g, span{text: fmt.Sprintf(" ↑%d", git.Ahead), style: th.fg(th.p.Cyan)})
		}
		if git.Behind > 0 {
			g = append(g, span{text: fmt.Sprintf(" ↓%d", git.Behind), style: th.fg(th.p.Amber)})
		}
	} else if level == 1 {
		if git.Conflicts > 0 {
			g = append(g, span{text: " ✖", style: th.fgBold(th.p.Red)})
		} else if git.Staged+git.Modified+git.Untracked > 0 || git.Dirty {
			g = append(g, span{text: " ●", style: th.fg(th.p.Amber)})
		}
	}
	return g
}

func buildActivityRightGroup(data *StatusLineData, level int, frame int64, th theme) group {
	if level < 2 {
		return nil
	}
	var g group
	addGap := func() {
		if len(g) > 0 {
			g = append(g, span{text: " ", style: ""})
		}
	}
	if activeSub := countActiveSubagents(data.Subagents); activeSub > 0 {
		addGap()
		g = append(g, span{text: fmt.Sprintf("◈%d", activeSub), style: th.fg(th.p.Purple)})
		if level >= 3 {
			eq := eqFrames[int(frame%int64(len(eqFrames)))]
			g = append(g, span{text: " " + eq, style: th.fg(th.p.Cyan)})
		}
	}
	if data.TaskCount > 0 {
		addGap()
		g = append(g, span{text: fmt.Sprintf("◷%d", data.TaskCount), style: th.fg(th.p.Cyan)})
	}
	if level >= 4 && data.ArtifactCount > 0 {
		addGap()
		g = append(g, span{text: fmt.Sprintf("✦%d", data.ArtifactCount), style: th.fg(th.p.Amber)})
	}
	return g
}

func packRightCandidates(candidates []rightCandidate, maxWidth int, th theme) (int, string) {
	if maxWidth <= 0 || len(candidates) == 0 {
		return 0, ""
	}
	byPri := append([]rightCandidate(nil), candidates...)
	sort.Slice(byPri, func(i, j int) bool { return byPri[i].priority < byPri[j].priority })

	var selected []rightCandidate
	usedW := 0
	for _, c := range byPri {
		cw := spansWidth(c.spans)
		needed := cw
		if len(selected) > 0 {
			needed += 3
		}
		if usedW+needed <= maxWidth {
			selected = append(selected, c)
			usedW += needed
		}
	}
	if len(selected) == 0 {
		return 0, ""
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].order < selected[j].order })

	var b strings.Builder
	totalW := 0
	for i, c := range selected {
		if i > 0 {
			totalW += 3
			if th.enabled {
				b.WriteString(th.fg(th.p.Dim))
				b.WriteString(" · ")
				b.WriteString(th.reset())
			} else {
				b.WriteString(" · ")
			}
		}
		w, s := renderSpans(c.spans, th)
		totalW += w
		b.WriteString(s)
	}
	return totalW, b.String()
}

// ── Row 2: Multi-Account agy-swap Fleet Deck Builder ───────────────────────

func renderRow2(
	quota map[string]QuotaBucket,
	swapInfo AgySwapInfo,
	family string,
	cols int,
	frame int64,
	th theme,
) string {
	if len(swapInfo.Fleet) == 0 {
		return ""
	}

	primary5h, has5h := lookupBucket(quota, family, "5h")
	primary7d, has7d := lookupBucket(quota, family, "weekly")
	primaryMinPct, hasPrimaryMin := poolMinRemainingPct(primary5h, has5h, primary7d, has7d)
	lowQuota := hasPrimaryMin && primaryMinPct < 25.0

	var bestReady *FleetAccountView
	for i := range swapInfo.Fleet {
		if swapInfo.Fleet[i].Status == "ready" {
			bestReady = &swapInfo.Fleet[i]
			break
		}
	}

	rightW, rightStr := renderFleetRightSummary(swapInfo.ReadyReserves, len(swapInfo.Fleet), bestReady, lowQuota, cols, th)
	availLeft := cols
	if rightW > 0 {
		availLeft = cols - rightW - 2
	}

	for chipStyle := 3; chipStyle >= 0; chipStyle-- {
		chips := make([]group, 0, len(swapInfo.Fleet))
		totalW := 0
		for i, fv := range swapInfo.Fleet {
			isTopSwap := lowQuota && bestReady != nil && strings.EqualFold(fv.Email, bestReady.Email)
			g := buildFleetChip(fv, family, chipStyle, isTopSwap, frame, th)
			gw := spansWidth(g)
			if i > 0 {
				totalW++
			}
			totalW += gw
			chips = append(chips, g)
		}
		if totalW <= availLeft {
			leftW, leftStr := joinChips(chips, th)
			if rightW > 0 {
				gap := cols - leftW - rightW
				if gap < 2 {
					gap = 2
				}
				return leftStr + strings.Repeat(" ", gap) + rightStr
			}
			return leftStr
		}
	}

	var fitted []group
	usedW := 0
	for idx, fv := range swapInfo.Fleet {
		isTopSwap := lowQuota && bestReady != nil && strings.EqualFold(fv.Email, bestReady.Email)
		g := buildFleetChip(fv, family, 0, isTopSwap, frame, th)
		gw := spansWidth(g)
		sep := 0
		if len(fitted) > 0 {
			sep = 1
		}
		rem := len(swapInfo.Fleet) - idx - 1
		reserveMore := 0
		if rem > 0 {
			reserveMore = 8
		}
		if usedW+sep+gw+reserveMore <= availLeft {
			fitted = append(fitted, g)
			usedW += sep + gw
		} else {
			leftCount := len(swapInfo.Fleet) - idx
			if leftCount > 0 && usedW+1+7 <= availLeft {
				fitted = append(fitted, group{{text: fmt.Sprintf("+%d more", leftCount), style: th.fg(th.p.Dim)}})
			}
			break
		}
	}
	if len(fitted) == 0 {
		return rightStr
	}
	leftW, leftStr := joinChips(fitted, th)
	if rightW > 0 {
		gap := cols - leftW - rightW
		if gap < 2 {
			gap = 2
		}
		return leftStr + strings.Repeat(" ", gap) + rightStr
	}
	return leftStr
}

func renderFleetRightSummary(readyCount, totalCount int, bestReady *FleetAccountView, lowQuota bool, cols int, th theme) (int, string) {
	if cols < 88 {
		return 0, ""
	}
	var g group
	if lowQuota && bestReady != nil {
		if cols >= 112 {
			bg := th.p.BgSwapPill
			name := shortAlias(bestReady.Alias, 12)
			g = group{
				{text: " ", style: th.bgOnly(bg)},
				{text: "⇄", style: th.style(th.p.Green, &bg, true, true)},
				{text: " SWAP READY ", style: th.style(th.p.Green, &bg, true, false)},
				{text: "▸ ", style: th.style(th.p.Dim, &bg, false, false)},
				{text: name, style: th.style(th.p.Text, &bg, true, false)},
				{text: fmt.Sprintf(" (%d/%d)", readyCount, totalCount), style: th.style(th.p.Muted, &bg, false, false)},
				{text: " ", style: th.bgOnly(bg)},
			}
		} else {
			name := shortAlias(bestReady.Alias, 10)
			g = group{
				{text: "⇄", style: th.style(th.p.Green, nil, true, true)},
				{text: " next ", style: th.fg(th.p.Green)},
				{text: "▸ ", style: th.fg(th.p.Dim)},
				{text: name, style: th.fg(th.p.Text)},
				{text: fmt.Sprintf(" (%d/%d)", readyCount, totalCount), style: th.fg(th.p.Muted)},
			}
		}
	} else {
		if cols >= 112 {
			g = group{
				{text: fmt.Sprintf("FLEET %d/%d", readyCount, totalCount), style: th.fg(th.p.Muted)},
				{text: " READY", style: th.fg(th.p.Green)},
			}
			if bestReady != nil {
				g = append(g,
					span{text: " · ", style: th.fg(th.p.Dim)},
					span{text: "next ", style: th.fg(th.p.Dim)},
					span{text: "▸ ", style: th.fg(th.p.Dim)},
					span{text: shortAlias(bestReady.Alias, 12), style: th.fg(th.p.Text)},
				)
			}
		} else {
			g = group{
				{text: fmt.Sprintf("FLEET %d/%d READY", readyCount, totalCount), style: th.fg(th.p.Muted)},
			}
		}
	}
	return renderSpans(g, th)
}

func buildFleetChip(fv FleetAccountView, family string, chipStyle int, isTopSwap bool, frame int64, th theme) group {
	_, secLabel := secondaryFamily(family)

	var bg *rgb
	if chipStyle >= 2 {
		if isTopSwap {
			b := th.p.BgSwapPill
			bg = &b
		} else {
			b := th.p.BgChipPill
			bg = &b
		}
	}

	styleFn := func(c rgb, bold, blink bool) string {
		return th.style(c, bg, bold, blink)
	}
	padStyle := func() string {
		if bg == nil {
			return ""
		}
		return th.bgOnly(*bg)
	}

	var maxAlias int
	switch chipStyle {
	case 3:
		maxAlias = 14
	case 2:
		maxAlias = 12
	case 1:
		maxAlias = 11
	default:
		maxAlias = 8
	}
	name := shortAlias(fv.Alias, maxAlias)

	var g group
	if bg != nil {
		g = append(g, span{text: " ", style: padStyle()})
	}

	switch fv.Status {
	case "ready":
		if isTopSwap {
			g = append(g, span{text: "⇄", style: styleFn(th.p.Green, true, true)})
		} else {
			g = append(g, span{text: "●", style: styleFn(th.p.Green, false, false)})
		}
		if chipStyle >= 1 {
			g = append(g, span{text: " ", style: padStyle()})
		}
		g = append(g, span{text: name, style: styleFn(th.p.Text, isTopSwap, false)})
		if chipStyle >= 3 && fv.Tier != "" {
			tc := th.p.Cyan
			if fv.Tier == "ULTRA" {
				tc = th.p.Purple
			}
			g = append(g, span{text: " " + fv.Tier, style: styleFn(tc, false, false)})
		}
		pctCol := th.p.Green
		if fv.PrimaryPct <= 25 {
			pctCol = th.p.Amber
		}
		if chipStyle >= 2 {
			g = append(g, span{text: " ", style: padStyle()})
			g = append(g, renderLaserBarOnBg(fv.PrimaryPct, 3, pctCol, bg, false, frame, th)...)
		}
		g = append(g, span{text: fmt.Sprintf(" %.0f%%", math.Round(fv.PrimaryPct)), style: styleFn(pctCol, isTopSwap, false)})

	case "partial":
		g = append(g, span{text: "◐", style: styleFn(th.p.Amber, false, false)})
		if chipStyle >= 1 {
			g = append(g, span{text: " ", style: padStyle()})
		}
		g = append(g, span{text: name, style: styleFn(th.p.Muted, false, false)})
		if chipStyle >= 3 && fv.Tier != "" {
			tc := th.p.Cyan
			if fv.Tier == "ULTRA" {
				tc = th.p.Purple
			}
			g = append(g, span{text: " " + fv.Tier, style: styleFn(tc, false, false)})
		}
		if fv.WaitSeconds > 0 {
			if fv.LowQuotaBlocked && fv.PrimaryPct > 0 {
				g = append(g,
					span{text: fmt.Sprintf(" %.0f%%", math.Round(fv.PrimaryPct)), style: styleFn(th.p.Red, false, false)},
					span{text: " ◷" + formatCompactSpan(fv.WaitSeconds), style: styleFn(th.p.Amber, false, false)},
				)
			} else {
				g = append(g, span{text: " ◷" + formatCompactSpan(fv.WaitSeconds), style: styleFn(th.p.Amber, false, false)})
			}
		} else {
			g = append(g, span{text: fmt.Sprintf(" %.0f%%", math.Round(fv.PrimaryPct)), style: styleFn(th.p.Amber, false, false)})
		}
		if chipStyle >= 2 && fv.HasSecondary {
			g = append(g,
				span{text: " · ", style: styleFn(th.p.Dim, false, false)},
				span{text: fmt.Sprintf("%s %.0f%%", secLabel, math.Round(fv.SecondaryPct)), style: styleFn(th.p.Green, false, false)},
			)
		}

	case "cooldown":
		g = append(g, span{text: "◌", style: styleFn(th.p.Dim, false, false)})
		if chipStyle >= 1 {
			g = append(g, span{text: " ", style: padStyle()})
		}
		g = append(g, span{text: name, style: styleFn(th.p.Muted, false, false)})
		if fv.WaitSeconds > 0 {
			if fv.LowQuotaBlocked && fv.PrimaryPct > 0 {
				g = append(g,
					span{text: fmt.Sprintf(" %.0f%%", math.Round(fv.PrimaryPct)), style: styleFn(th.p.Red, false, false)},
					span{text: " ◷" + formatCompactSpan(fv.WaitSeconds), style: styleFn(th.p.Amber, false, false)},
				)
			} else {
				g = append(g, span{text: " ◷" + formatCompactSpan(fv.WaitSeconds), style: styleFn(th.p.Amber, false, false)})
			}
		} else {
			g = append(g, span{text: fmt.Sprintf(" %.0f%%", math.Round(fv.PrimaryPct)), style: styleFn(th.p.Red, false, false)})
		}

	default:
		g = append(g, span{text: "◌", style: styleFn(th.p.Dim, false, false)})
		if chipStyle >= 1 {
			g = append(g, span{text: " ", style: padStyle()})
		}
		g = append(g,
			span{text: name, style: styleFn(th.p.Dim, false, false)},
			span{text: " ✕", style: styleFn(th.p.Red, false, false)},
		)
	}

	if bg != nil {
		g = append(g, span{text: " ", style: padStyle()})
	}
	return g
}

func joinChips(chips []group, th theme) (int, string) {
	var b strings.Builder
	totalW := 0
	for i, g := range chips {
		if i > 0 {
			b.WriteByte(' ')
			totalW++
		}
		w, s := renderSpans(g, th)
		totalW += w
		b.WriteString(s)
	}
	return totalW, b.String()
}

// ── Bucket / Color / Workspace Helpers ─────────────────────────────────────

func lookupBucket(quota map[string]QuotaBucket, family, window string) (QuotaBucket, bool) {
	if len(quota) == 0 {
		return QuotaBucket{}, false
	}
	candidates := []string{
		family + "-" + window,
		family + "_" + window,
	}
	if family == "3p" {
		candidates = append(candidates, "third_party-"+window, "third_party_"+window, "claude-"+window)
	}
	for _, key := range candidates {
		if b, ok := quota[key]; ok {
			if b.Disabled || b.RemainingFraction == nil {
				return QuotaBucket{}, false
			}
			return b, true
		}
	}
	for k, b := range quota {
		lk := strings.ToLower(k)
		if (strings.Contains(lk, family) || (family == "3p" && strings.Contains(lk, "claude"))) &&
			(strings.Contains(lk, window) || (window == "weekly" && strings.Contains(lk, "7d"))) {
			if b.Disabled || b.RemainingFraction == nil {
				return QuotaBucket{}, false
			}
			return b, true
		}
	}
	return QuotaBucket{}, false
}

func poolMinRemainingPct(b5h QuotaBucket, has5h bool, b7d QuotaBucket, has7d bool) (float64, bool) {
	if !has5h && !has7d {
		return 0, false
	}
	if has5h && !has7d {
		return bucketRemainingPct(b5h), true
	}
	if !has5h && has7d {
		return bucketRemainingPct(b7d), true
	}
	p5 := bucketRemainingPct(b5h)
	p7 := bucketRemainingPct(b7d)
	if p5 <= p7 {
		return p5, true
	}
	return p7, true
}

func bucketRemainingPct(b QuotaBucket) float64 {
	if b.RemainingFraction == nil {
		return 0
	}
	return clamp(*b.RemainingFraction*100.0, 0, 100)
}

func bucketResetSeconds(b QuotaBucket, now time.Time) (int64, bool) {
	if b.ResetInSeconds != nil {
		return *b.ResetInSeconds, true
	}
	if b.ResetTime != nil && strings.TrimSpace(*b.ResetTime) != "" {
		if ts, ok := parseTimestamp(*b.ResetTime); ok {
			sec := int64(ts.Sub(now).Seconds())
			if sec < 0 {
				sec = 0
			}
			return sec, true
		}
	}
	return 0, false
}

func quotaColorRGB(remPct float64, b QuotaBucket, windowSec int64, now time.Time, th theme) (rgb, bool) {
	paceWarn := false
	if remSec, ok := bucketResetSeconds(b, now); ok && windowSec > 0 && remSec > 0 && remSec <= windowSec {
		elapsedPct := (1.0 - float64(remSec)/float64(windowSec)) * 100.0
		usedPct := 100.0 - remPct
		if usedPct-elapsedPct >= 15.0 {
			paceWarn = true
		}
	}
	if remPct <= 20.0 {
		return th.p.Red, paceWarn
	}
	if remPct <= 45.0 {
		return th.p.Amber, paceWarn
	}
	if paceWarn {
		return th.p.Amber, true
	}
	return th.p.Green, false
}

func ctxColorRGB(pct float64, th theme) rgb {
	if pct >= 80.0 {
		return th.p.Red
	}
	if pct >= 55.0 {
		return th.p.Amber
	}
	return th.p.Blue
}

func renderPie(pct float64) string {
	switch {
	case pct >= 88:
		return "●"
	case pct >= 63:
		return "◕"
	case pct >= 38:
		return "◑"
	case pct >= 13:
		return "◔"
	default:
		return "○"
	}
}

func workspaceLabel(data *StatusLineData, level int, getenv func(string) string) string {
	curDir := ""
	projDir := ""
	if data.Workspace != nil {
		curDir = strings.TrimSpace(data.Workspace.CurrentDir)
		projDir = strings.TrimSpace(data.Workspace.ProjectDir)
	}
	if curDir == "" {
		curDir = strings.TrimSpace(data.CWD)
	}
	if curDir == "" && projDir == "" {
		return ""
	}
	home := ""
	if getenv != nil {
		home = strings.TrimSpace(getenv("HOME"))
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home != "" && curDir != "" && filepath.Clean(curDir) == filepath.Clean(home) {
		return "~"
	}

	baseProj := filepath.Base(filepath.Clean(firstNonEmpty(projDir, curDir)))
	if baseProj == "." || baseProj == "/" {
		baseProj = ""
	}
	if level >= 4 && projDir != "" && curDir != "" && filepath.Clean(curDir) != filepath.Clean(projDir) {
		if rel, err := filepath.Rel(filepath.Clean(projDir), filepath.Clean(curDir)); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			candidate := baseProj + "/" + filepath.ToSlash(rel)
			if DisplayWidth(candidate) <= 24 {
				return candidate
			}
		}
		return clip(baseProj, 24)
	}
	maxLen := 18
	if level >= 4 {
		maxLen = 24
	}
	return clip(baseProj, maxLen)
}

func countActiveSubagents(subs []SubagentInfo) int {
	n := 0
	for _, s := range subs {
		st := strings.ToLower(strings.TrimSpace(s.Status))
		switch st {
		case "done", "completed", "killed", "cancelled", "canceled", "failed", "error":
			continue
		default:
			n++
		}
	}
	return n
}

func spansWidth(g group) int {
	w := 0
	for _, sp := range g {
		w += DisplayWidth(sp.text)
	}
	return w
}

func renderSpans(g group, th theme) (int, string) {
	var b strings.Builder
	plainW := 0
	for _, sp := range g {
		plainW += DisplayWidth(sp.text)
		if sp.style != "" && th.enabled {
			b.WriteString(sp.style)
			b.WriteString(sp.text)
			b.WriteString(th.reset())
		} else {
			b.WriteString(sp.text)
		}
	}
	return plainW, b.String()
}

func renderGroups(groups []group, th theme) (int, string) {
	var b strings.Builder
	plainWidth := 0
	for i, g := range groups {
		if i > 0 {
			b.WriteString("  ")
			plainWidth += 2
		}
		w, s := renderSpans(g, th)
		plainWidth += w
		b.WriteString(s)
	}
	return plainWidth, b.String()
}
