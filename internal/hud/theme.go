package hud

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type rgb struct {
	r, g, b int
}

// ThemePalette defines the 24-bit color tokens and pill backgrounds for a HUD theme.
type ThemePalette struct {
	Name         string
	Mono         bool
	Text         rgb
	Muted        rgb
	Dim          rgb
	Track        rgb
	Orange       rgb
	Cyan         rgb
	Blue         rgb
	Green        rgb
	Amber        rgb
	Red          rgb
	Purple       rgb
	TipBlue      rgb
	TipGreen     rgb
	TipAmber     rgb
	TipRed       rgb
	BgActivePill rgb
	BgWorkPill   rgb
	BgAlertPill  rgb
	BgSwapPill   rgb
	BgChipPill   rgb
}

var builtinPalettes = map[string]ThemePalette{
	"tokyo-night": {
		Name:         "tokyo-night",
		Text:         rgb{192, 202, 245}, // #c0caf5
		Muted:        rgb{120, 124, 153}, // #787c99
		Dim:          rgb{86, 95, 137},   // #565f89
		Track:        rgb{52, 59, 84},    // #343b54
		Orange:       rgb{255, 158, 100}, // #ff9e64
		Cyan:         rgb{125, 207, 255}, // #7dcfff
		Blue:         rgb{122, 162, 247}, // #7aa2f7
		Green:        rgb{115, 218, 202}, // #73daca
		Amber:        rgb{224, 175, 104}, // #e0af68
		Red:          rgb{247, 118, 142}, // #f7768e
		Purple:       rgb{187, 154, 247}, // #bb9af7
		TipBlue:      rgb{180, 220, 255},
		TipGreen:     rgb{158, 240, 224},
		TipAmber:     rgb{255, 212, 138},
		TipRed:       rgb{255, 163, 179},
		BgActivePill: rgb{26, 31, 48}, // #1a1f30
		BgWorkPill:   rgb{28, 40, 68}, // #1c2844
		BgAlertPill:  rgb{58, 34, 24}, // #3a2218
		BgSwapPill:   rgb{19, 38, 36}, // #132624
		BgChipPill:   rgb{20, 23, 34}, // #141722
	},
	"catppuccin": {
		Name:         "catppuccin",
		Text:         rgb{205, 214, 244}, // #cdd6f4
		Muted:        rgb{166, 173, 200}, // #a6adc8
		Dim:          rgb{108, 112, 134}, // #6c7086
		Track:        rgb{69, 71, 90},    // #45475a
		Orange:       rgb{250, 179, 135}, // #fab387
		Cyan:         rgb{137, 220, 235}, // #89dceb
		Blue:         rgb{137, 180, 250}, // #89b4fa
		Green:        rgb{166, 227, 161}, // #a6e3a1
		Amber:        rgb{249, 226, 175}, // #f9e2af
		Red:          rgb{243, 139, 168}, // #f38ba8
		Purple:       rgb{203, 166, 247}, // #cba6f7
		TipBlue:      rgb{194, 222, 255},
		TipGreen:     rgb{202, 245, 198},
		TipAmber:     rgb{255, 240, 200},
		TipRed:       rgb{255, 184, 204},
		BgActivePill: rgb{30, 30, 46}, // #1e1e2e
		BgWorkPill:   rgb{36, 40, 66},
		BgAlertPill:  rgb{58, 36, 36},
		BgSwapPill:   rgb{24, 42, 36},
		BgChipPill:   rgb{24, 24, 37}, // #181825
	},
	"gruvbox": {
		Name:         "gruvbox",
		Text:         rgb{235, 219, 178}, // #ebdbb2
		Muted:        rgb{189, 174, 147}, // #bdae93
		Dim:          rgb{146, 131, 116}, // #928374
		Track:        rgb{80, 73, 69},    // #504945
		Orange:       rgb{254, 128, 25},  // #fe8019
		Cyan:         rgb{142, 192, 124}, // #8ec07c
		Blue:         rgb{131, 165, 152}, // #83a598
		Green:        rgb{184, 187, 38},  // #b8bb26
		Amber:        rgb{250, 189, 47},  // #fabd2f
		Red:          rgb{251, 73, 52},   // #fb4934
		Purple:       rgb{211, 134, 155}, // #d3869b
		TipBlue:      rgb{182, 214, 202},
		TipGreen:     rgb{218, 224, 108},
		TipAmber:     rgb{255, 218, 122},
		TipRed:       rgb{255, 132, 112},
		BgActivePill: rgb{40, 40, 40}, // #282828
		BgWorkPill:   rgb{48, 52, 50},
		BgAlertPill:  rgb{66, 36, 26},
		BgSwapPill:   rgb{38, 48, 30},
		BgChipPill:   rgb{32, 32, 32},
	},
	"nord": {
		Name:         "nord",
		Text:         rgb{236, 239, 244}, // #eceff4
		Muted:        rgb{216, 222, 233}, // #d8dee9
		Dim:          rgb{126, 142, 170}, // #7e8eaa
		Track:        rgb{67, 76, 94},    // #434c5e
		Orange:       rgb{208, 135, 112}, // #d08770
		Cyan:         rgb{136, 192, 208}, // #88c0d0
		Blue:         rgb{129, 161, 193}, // #81a1c1
		Green:        rgb{163, 190, 140}, // #a3be8c
		Amber:        rgb{235, 203, 139}, // #ebcb8b
		Red:          rgb{191, 97, 106},  // #bf616a
		Purple:       rgb{180, 142, 173}, // #b48ead
		TipBlue:      rgb{182, 212, 235},
		TipGreen:     rgb{198, 222, 178},
		TipAmber:     rgb{248, 226, 178},
		TipRed:       rgb{228, 146, 154},
		BgActivePill: rgb{46, 52, 64}, // #2e3440
		BgWorkPill:   rgb{52, 62, 82},
		BgAlertPill:  rgb{72, 46, 48},
		BgSwapPill:   rgb{42, 62, 54},
		BgChipPill:   rgb{38, 43, 54},
	},
	"dracula": {
		Name:         "dracula",
		Text:         rgb{248, 248, 242}, // #f8f8f2
		Muted:        rgb{189, 194, 222},
		Dim:          rgb{98, 114, 164},  // #6272a4
		Track:        rgb{68, 71, 90},    // #44475a
		Orange:       rgb{255, 184, 108}, // #ffb86c
		Cyan:         rgb{139, 233, 253}, // #8be9fd
		Blue:         rgb{139, 233, 253},
		Green:        rgb{80, 250, 123},  // #50fa7b
		Amber:        rgb{241, 250, 140}, // #f1fa8c
		Red:          rgb{255, 85, 85},   // #ff5555
		Purple:       rgb{189, 147, 249}, // #bd93f9
		TipBlue:      rgb{194, 245, 255},
		TipGreen:     rgb{152, 255, 180},
		TipAmber:     rgb{250, 255, 186},
		TipRed:       rgb{255, 148, 148},
		BgActivePill: rgb{40, 42, 54}, // #282a36
		BgWorkPill:   rgb{48, 52, 74},
		BgAlertPill:  rgb{74, 38, 38},
		BgSwapPill:   rgb{28, 52, 42},
		BgChipPill:   rgb{33, 35, 45},
	},
	"mono": {
		Name:         "mono",
		Mono:         true,
		Text:         rgb{238, 238, 242},
		Muted:        rgb{176, 178, 186},
		Dim:          rgb{116, 118, 126},
		Track:        rgb{64, 66, 74},
		Orange:       rgb{245, 245, 248},
		Cyan:         rgb{232, 232, 236},
		Blue:         rgb{218, 218, 224},
		Green:        rgb{228, 228, 234},
		Amber:        rgb{242, 242, 246},
		Red:          rgb{255, 255, 255},
		Purple:       rgb{206, 206, 214},
		TipBlue:      rgb{255, 255, 255},
		TipGreen:     rgb{255, 255, 255},
		TipAmber:     rgb{255, 255, 255},
		TipRed:       rgb{255, 255, 255},
		BgActivePill: rgb{28, 29, 34},
		BgWorkPill:   rgb{36, 38, 44},
		BgAlertPill:  rgb{52, 52, 58},
		BgSwapPill:   rgb{34, 38, 38},
		BgChipPill:   rgb{22, 23, 27},
	},
}

// BuiltinThemeNames returns the supported theme identifiers in display order.
func BuiltinThemeNames() []string {
	return []string{"tokyo-night", "catppuccin", "gruvbox", "nord", "dracula", "mono"}
}

// NormalizeThemeName maps user/settings colorScheme labels to a canonical theme key.
func NormalizeThemeName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	switch {
	case s == "" || strings.Contains(s, "tokyo") || s == "dark" || s == "default":
		return "tokyo-night"
	case strings.Contains(s, "catppuccin"):
		return "catppuccin"
	case strings.Contains(s, "gruvbox"):
		return "gruvbox"
	case strings.Contains(s, "nord"):
		return "nord"
	case strings.Contains(s, "dracula"):
		return "dracula"
	case strings.Contains(s, "mono") || strings.Contains(s, "plain"):
		return "mono"
	default:
		if _, ok := builtinPalettes[s]; ok {
			return s
		}
		return "tokyo-night"
	}
}

// ResolveThemeName selects a theme via explicit override, env var, or agy colorScheme.
func ResolveThemeName(explicit string, getenv func(string) string) string {
	if strings.TrimSpace(explicit) != "" {
		return NormalizeThemeName(explicit)
	}
	if getenv != nil {
		if envTheme := strings.TrimSpace(getenv("AGY_STATUSLINE_THEME")); envTheme != "" {
			return NormalizeThemeName(envTheme)
		}
	}
	if scheme := readSettingsColorScheme(getenv); scheme != "" {
		return NormalizeThemeName(scheme)
	}
	return "tokyo-night"
}

func readSettingsColorScheme(getenv func(string) string) string {
	var path string
	if getenv != nil {
		path = strings.TrimSpace(getenv("AGY_SETTINGS_PATH"))
	}
	if path == "" {
		home := ""
		if getenv != nil {
			home = strings.TrimSpace(getenv("HOME"))
		}
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		if home == "" {
			return ""
		}
		path = filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var doc struct {
		ColorScheme string `json:"colorScheme"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	return doc.ColorScheme
}

type theme struct {
	enabled bool
	noBlink bool
	p       ThemePalette
}

func newTheme(name string, enabled, noBlink bool) theme {
	key := NormalizeThemeName(name)
	pal, ok := builtinPalettes[key]
	if !ok {
		pal = builtinPalettes["tokyo-night"]
	}
	return theme{
		enabled: enabled,
		noBlink: noBlink,
		p:       pal,
	}
}

func (t theme) style(fg rgb, bg *rgb, bold, blink bool) string {
	if !t.enabled {
		return ""
	}
	var codes []string
	if bold {
		codes = append(codes, "1")
	}
	if blink && !t.noBlink {
		codes = append(codes, "5")
	}
	codes = append(codes, fmt.Sprintf("38;2;%d;%d;%d", fg.r, fg.g, fg.b))
	if bg != nil {
		codes = append(codes, fmt.Sprintf("48;2;%d;%d;%d", bg.r, bg.g, bg.b))
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

func (t theme) bgOnly(bg rgb) string {
	if !t.enabled {
		return ""
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", bg.r, bg.g, bg.b)
}

func (t theme) reset() string {
	if !t.enabled {
		return ""
	}
	return "\x1b[0m"
}

func (t theme) fg(c rgb) string     { return t.style(c, nil, false, false) }
func (t theme) fgBold(c rgb) string { return t.style(c, nil, true, false) }

type span struct {
	text  string
	style string
}

type group []span

func renderLaserBar(pct float64, cells int, fillColor rgb, working bool, frame int64, th theme) group {
	return renderLaserBarOnBg(pct, cells, fillColor, nil, working, frame, th)
}

func renderLaserBarOnBg(pct float64, cells int, fillColor rgb, bg *rgb, working bool, frame int64, th theme) group {
	if cells <= 0 {
		return nil
	}
	halfSteps := int(math.Round(clamp(pct, 0, 100) / 100.0 * float64(cells*2)))
	if pct > 0 && halfSteps == 0 {
		halfSteps = 1
	}
	if halfSteps > cells*2 {
		halfSteps = cells * 2
	}
	fullCells := halfSteps / 2
	hasHalf := (halfSteps % 2) == 1
	emptyCells := cells - fullCells
	if hasHalf {
		emptyCells--
	}
	if emptyCells < 0 {
		emptyCells = 0
	}

	tipColor := th.brightenTip(fillColor)
	pulseTip := working && (frame%2 == 0)

	var g group
	if fullCells > 0 {
		if pulseTip {
			if fullCells > 1 {
				g = append(g, span{text: strings.Repeat("━", fullCells-1), style: th.style(fillColor, bg, false, false)})
			}
			if !hasHalf {
				g = append(g, span{text: "━", style: th.style(tipColor, bg, true, false)})
			} else {
				g = append(g, span{text: "━", style: th.style(fillColor, bg, false, false)})
			}
		} else {
			g = append(g, span{text: strings.Repeat("━", fullCells), style: th.style(fillColor, bg, false, false)})
		}
	}
	if hasHalf {
		c := fillColor
		if pulseTip {
			c = tipColor
		}
		g = append(g, span{text: "╸", style: th.style(c, bg, pulseTip, false)})
	}
	if emptyCells > 0 {
		g = append(g, span{text: strings.Repeat("─", emptyCells), style: th.style(th.p.Track, bg, false, false)})
	}
	return g
}

func (t theme) brightenTip(c rgb) rgb {
	switch c {
	case t.p.Blue:
		return t.p.TipBlue
	case t.p.Green:
		return t.p.TipGreen
	case t.p.Amber:
		return t.p.TipAmber
	case t.p.Red:
		return t.p.TipRed
	default:
		return t.p.Text
	}
}

func shimmerSpans(text string, bg rgb, frame int64, th theme) group {
	if !th.enabled {
		return group{{text: text}}
	}
	palette := []rgb{th.p.Cyan, th.p.Blue, th.p.Purple}
	var g group
	idx := 0
	for _, r := range text {
		phase := float64(frame)*0.65 - float64(idx)*0.45
		pos := 1.0 - math.Cos(phase)
		var c rgb
		if pos <= 1.0 {
			c = lerpRGB(palette[0], palette[1], pos)
		} else {
			c = lerpRGB(palette[1], palette[2], pos-1.0)
		}
		g = append(g, span{text: string(r), style: th.style(c, &bg, true, false)})
		idx++
	}
	return g
}

func lerpRGB(a, b rgb, t float64) rgb {
	t = clamp(t, 0, 1)
	return rgb{
		r: int(math.Round(float64(a.r) + (float64(b.r)-float64(a.r))*t)),
		g: int(math.Round(float64(a.g) + (float64(b.g)-float64(a.g))*t)),
		b: int(math.Round(float64(a.b) + (float64(b.b)-float64(a.b))*t)),
	}
}

// SamplePreview renders a short sample pill + laser gauges for the named theme.
func SamplePreview(name string, colorEnabled bool) string {
	th := newTheme(name, colorEnabled, true)
	bg := th.p.BgActivePill
	var g group
	g = append(g,
		span{text: " ", style: th.bgOnly(bg)},
		span{text: "⬢ ", style: th.style(th.p.Orange, &bg, false, false)},
		span{text: fmt.Sprintf("%-11s", th.p.Name), style: th.style(th.p.Text, &bg, true, false)},
		span{text: " ", style: th.bgOnly(bg)},
		span{text: "  5h ", style: th.fg(th.p.Dim)},
	)
	g = append(g, renderLaserBar(76, 6, th.p.Green, false, 0, th)...)
	g = append(g, span{text: " 76%", style: th.fg(th.p.Green)})
	g = append(g, span{text: "  CTX ", style: th.fg(th.p.Muted)})
	g = append(g, renderLaserBar(38, 6, th.p.Blue, false, 0, th)...)
	g = append(g, span{text: " 38%", style: th.fg(th.p.Blue)})
	_, out := renderSpans(g, th)
	return out
}
