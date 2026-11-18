package hud

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func isTruthy(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	return s == "1" || s == "true" || s == "yes"
}

func formatTokens(n int) string {
	if n >= 1_000_000 {
		if n%1_000_000 == 0 || n >= 10_000_000 {
			return fmt.Sprintf("%.0fM", float64(n)/1_000_000.0)
		}
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000.0)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%dk", int(math.Round(float64(n)/1_000.0)))
	}
	return strconv.Itoa(n)
}

func formatMoney(usd float64) string {
	if usd >= 100 {
		return fmt.Sprintf("$%.0f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

func formatCompactSpan(seconds int64) string {
	if seconds < 60 {
		return "<1m"
	}
	m := int64(math.Round(float64(seconds) / 60.0))
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	if m < 1440 {
		h := m / 60
		rm := m % 60
		return fmt.Sprintf("%dh%02dm", h, rm)
	}
	d := m / 1440
	rh := (m % 1440) / 60
	return fmt.Sprintf("%dd%dh", d, rh)
}

func parseTimestamp(raw string) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

func runeWidth(r rune) int {
	if r == 0 || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if isWideRune(r) {
		return 2
	}
	return 1
}

func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r == 0x2329 || r == 0x232A,
		r >= 0x2E80 && r <= 0x303E,
		r >= 0x3040 && r <= 0x33FF,
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x4E00 && r <= 0x9FFF,
		r >= 0xA000 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7AF,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE10 && r <= 0xFE19,
		r >= 0xFE30 && r <= 0xFE6F,
		r >= 0xFF01 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1FAFF,
		r >= 0x20000 && r <= 0x3FFFF:
		return true
	default:
		return false
	}
}

// DisplayWidth measures terminal column width ignoring zero-width combining marks.
func DisplayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func clip(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	if DisplayWidth(s) <= maxCols {
		return s
	}
	if maxCols == 1 {
		return "…"
	}
	limit := maxCols - 1
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := runeWidth(r)
		if rw > 0 && w+rw > limit {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	b.WriteRune('…')
	return b.String()
}

func shortAlias(name string, maxCols int) string {
	name = strings.TrimSpace(name)
	if name == "" || maxCols <= 0 {
		return ""
	}
	if DisplayWidth(name) <= maxCols {
		return name
	}
	fields := strings.Fields(name)
	if len(fields) >= 2 && DisplayWidth(fields[0]) >= 4 && DisplayWidth(fields[0]) <= maxCols {
		r, _ := utf8.DecodeRuneInString(fields[1])
		if r != utf8.RuneError {
			candidate := fields[0] + " " + string(r) + "."
			if DisplayWidth(candidate) <= maxCols {
				return candidate
			}
		}
		return fields[0]
	}
	return clip(name, maxCols)
}

func hardTruncateANSI(styled string, maxCols int, hasColor bool) string {
	if maxCols <= 0 {
		return ""
	}
	if maxCols == 1 {
		if hasColor {
			return "…\x1b[0m"
		}
		return "…"
	}
	limit := maxCols - 1
	var b strings.Builder
	w := 0
	inEsc := false
	truncated := false
	for _, r := range styled {
		if inEsc {
			b.WriteRune(r)
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		rw := runeWidth(r)
		if rw > 0 && w+rw > limit {
			truncated = true
			break
		}
		b.WriteRune(r)
		w += rw
	}
	if truncated {
		b.WriteRune('…')
		if hasColor {
			b.WriteString("\x1b[0m")
		}
	}
	return b.String()
}

func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
