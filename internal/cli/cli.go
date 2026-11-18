package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aklkbqx/agy-statusline/internal/hud"
)

// Run executes the requested subcommand or interactive preview.
func Run(
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	version string,
	buildID string,
	getenv func(string) string,
) int {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if len(args) == 0 {
		return RunInteractiveNoArgs(stdout, version, buildID, getenv)
	}

	switch args[0] {
	case "render":
		out := hud.RenderReader(stdin, time.Now().UTC(), getenv)
		if out != "" {
			fmt.Fprintln(stdout, out)
		}
		return 0
	case "install":
		return cmdInstall(args[1:], stdout, stderr, getenv)
	case "uninstall":
		return cmdUninstall(args[1:], stdout, stderr, getenv)
	case "doctor":
		return cmdDoctor(args[1:], stdout, stderr, version, buildID, getenv)
	case "demo":
		return cmdDemo(args[1:], stdout, stderr, getenv)
	case "themes":
		return cmdThemes(stdout, getenv)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "agy-statusline v%s (%s)\n", version, buildID)
		return 0
	case "help", "--help", "-h":
		printHelp(stdout, version)
		return 0
	default:
		fmt.Fprintf(stderr, "agy-statusline: unknown command %q\n\n", args[0])
		printHelp(stderr, version)
		return 2
	}
}

// RunInteractiveNoArgs prints a live preview and onboarding hint when invoked directly in a terminal.
func RunInteractiveNoArgs(stdout io.Writer, version, buildID string, getenv func(string) string) int {
	fmt.Fprintf(stdout, "agy-statusline v%s (%s)\n\n", version, buildID)
	width := resolveDemoWidth(0, getenv)
	themeName := hud.ResolveThemeName("", getenv)
	fmt.Fprintln(stdout, hud.RenderDemo(width, "idle", themeName, false, time.Now().UTC(), getenv))
	fmt.Fprintln(stdout, "\nRun 'agy-statusline install' to enable in Antigravity CLI, or 'agy-statusline --help' for options.")
	return 0
}

func cmdInstall(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	replace := fs.Bool("replace", false, "Replace agy's built-in status line instead of stacking below it")
	settingsFlag := fs.String("settings", "", "Override path to Antigravity CLI settings.json")
	binFlag := fs.String("bin", "", "Override executable path recorded in settings.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	settingsPath := strings.TrimSpace(*settingsFlag)
	if settingsPath == "" {
		settingsPath = DefaultSettingsPath(getenv)
	}

	binPath := strings.TrimSpace(*binFlag)
	if binPath == "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(stderr, "agy-statusline install: cannot locate binary: %v\n", err)
			return 1
		}
		binPath = exe
	}

	stackWithDefault := !*replace
	if err := InstallSettings(settingsPath, binPath, stackWithDefault); err != nil {
		fmt.Fprintf(stderr, "agy-statusline install: %v\n", err)
		return 1
	}

	mode := "stacked below built-in statusLine"
	if *replace {
		mode = "replacing built-in statusLine"
	}
	fmt.Fprintf(stdout, "✓ Updated %s\n", settingsPath)
	fmt.Fprintf(stdout, "  command : %s\n", binPath)
	fmt.Fprintf(stdout, "  mode    : %s (stack_with_default=%v)\n\n", mode, stackWithDefault)
	fmt.Fprintln(stdout, hud.RenderDemo(resolveDemoWidth(120, getenv), "idle", hud.ResolveThemeName("", getenv), false, time.Now().UTC(), getenv))
	fmt.Fprintln(stdout, "\nRestart any open 'agy' session (or start a new one) if the updated bar does not appear immediately.")
	return 0
}

func cmdUninstall(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	settingsFlag := fs.String("settings", "", "Override path to Antigravity CLI settings.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	settingsPath := strings.TrimSpace(*settingsFlag)
	if settingsPath == "" {
		settingsPath = DefaultSettingsPath(getenv)
	}

	changed, err := UninstallSettings(settingsPath)
	if err != nil {
		fmt.Fprintf(stderr, "agy-statusline uninstall: %v\n", err)
		return 1
	}
	if !changed {
		fmt.Fprintf(stdout, "Nothing to change (%s does not exist).\n", settingsPath)
		return 0
	}
	fmt.Fprintf(stdout, "✓ Reverted statusLine in %s\n", settingsPath)
	return 0
}

func cmdDoctor(args []string, stdout, stderr io.Writer, version, buildID string, getenv func(string) string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	settingsFlag := fs.String("settings", "", "Override path to Antigravity CLI settings.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	settingsPath := strings.TrimSpace(*settingsFlag)
	if settingsPath == "" {
		settingsPath = DefaultSettingsPath(getenv)
	}

	exePath := "unknown"
	if exe, err := os.Executable(); err == nil {
		exePath = exe
	}

	rep := InspectSettings(settingsPath)
	agyPath := findAgyBinary(getenv)
	gitPath, gitErr := exec.LookPath("git")

	swapDir := hud.ResolveAgySwapDir(getenv)
	swapInfo := hud.LoadAgySwapInfo(nil, "gemini", nil, time.Now().UTC(), getenv)

	resolvedTheme := hud.ResolveThemeName("", getenv)
	termVal := getenv("TERM")
	colorTerm := getenv("COLORTERM")
	noColor := getenv("NO_COLOR")

	fmt.Fprintf(stdout, "agy-statusline doctor (v%s, %s)\n", version, buildID)
	fmt.Fprintf(stdout, "  binary      : %s\n", exePath)
	fmt.Fprintf(stdout, "  state dir   : %s\n", hud.ResolveStateDir(getenv))

	if !rep.Exists {
		fmt.Fprintf(stdout, "  settings    : missing (%s)\n", rep.Path)
	} else if !rep.Valid {
		fmt.Fprintf(stdout, "  settings    : INVALID JSON (%s)\n", rep.Path)
	} else {
		enabled := rep.StatusLine.Enabled == nil || *rep.StatusLine.Enabled
		fmt.Fprintf(stdout, "  settings    : valid (%s)\n", rep.Path)
		fmt.Fprintf(stdout, "  statusLine  : type=%q enabled=%v stack_with_default=%v command=%q\n",
			rep.StatusLine.Type, enabled, rep.StatusLine.StackWithDefault, rep.StatusLine.Command)
	}

	if agyPath != "" {
		fmt.Fprintf(stdout, "  agy CLI     : %s\n", agyPath)
	} else {
		fmt.Fprintln(stdout, "  agy CLI     : not found on PATH or ~/.local/libexec/agy")
	}

	if gitErr == nil {
		fmt.Fprintf(stdout, "  git         : %s\n", gitPath)
	} else {
		fmt.Fprintln(stdout, "  git         : not found")
	}

	if swapInfo.TotalAccounts > 0 {
		fmt.Fprintf(stdout, "  agy-swap    : detected (%d accounts, %d ready for Gemini; 0 secrets read) [%s]\n",
			swapInfo.TotalAccounts, swapInfo.ReadyReserves, swapDir)
	} else {
		fmt.Fprintf(stdout, "  agy-swap    : not configured (%s)\n", swapDir)
	}

	fmt.Fprintf(stdout, "  theme       : %s (colorScheme=%q COLORTERM=%q TERM=%q NO_COLOR=%q)\n",
		resolvedTheme, rep.ColorScheme, colorTerm, termVal, noColor)

	if rep.Exists && !rep.Valid {
		return 1
	}
	return 0
}

func findAgyBinary(getenv func(string) string) string {
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	home := strings.TrimSpace(getenv("HOME"))
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home != "" {
		for _, cand := range []string{
			filepath.Join(home, ".local", "bin", "agy"),
			filepath.Join(home, ".local", "libexec", "agy"),
		} {
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				return cand
			}
		}
	}
	return ""
}

func cmdDemo(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	width := fs.Int("width", 0, "Target terminal width (default: COLUMNS or 142)")
	state := fs.String("state", "idle", "Agent state preview: idle | thinking | confirm")
	themeFlag := fs.String("theme", "", "Theme name (see 'agy-statusline themes')")
	singleLine := fs.Bool("single-line", false, "Render compact 1-line mode")
	all := fs.Bool("all", false, "Render 3-tier responsive showcase (142, 96, 52 cols)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	themeName := hud.ResolveThemeName(*themeFlag, getenv)
	now := time.Now().UTC()

	if *all {
		for _, w := range []int{142, 96, 52} {
			fmt.Fprintf(stdout, "[%d cols · %s]\n", w, *state)
			fmt.Fprintln(stdout, hud.RenderDemo(w, *state, themeName, *singleLine, now, getenv))
			fmt.Fprintln(stdout)
		}
		return 0
	}

	w := resolveDemoWidth(*width, getenv)
	fmt.Fprintln(stdout, hud.RenderDemo(w, *state, themeName, *singleLine, now, getenv))
	return 0
}

func resolveDemoWidth(requested int, getenv func(string) string) int {
	if requested > 0 {
		return requested
	}
	if getenv != nil {
		if c := strings.TrimSpace(getenv("COLUMNS")); c != "" {
			if n, err := strconv.Atoi(c); err == nil && n > 0 {
				return n
			}
		}
	}
	return 142
}

func cmdThemes(stdout io.Writer, getenv func(string) string) int {
	colorEnabled := getenv("NO_COLOR") == "" && !strings.EqualFold(getenv("TERM"), "dumb")
	active := hud.ResolveThemeName("", getenv)
	fmt.Fprintln(stdout, "Available themes (set via AGY_STATUSLINE_THEME or agy colorScheme):")
	for _, name := range hud.BuiltinThemeNames() {
		mark := " "
		if name == active {
			mark = "*"
		}
		fmt.Fprintf(stdout, " %s %s\n", mark, hud.SamplePreview(name, colorEnabled))
	}
	return 0
}

func printHelp(w io.Writer, version string) {
	fmt.Fprintf(w, `agy-statusline v%s — Responsive 2-row statusline HUD for Google Antigravity CLI (agy)

Usage:
  agy-statusline                  Read StatusLineData JSON from stdin (or show demo on a TTY)
  agy-statusline render           Explicitly read StatusLineData JSON from stdin
  agy-statusline install          Register statusLine in ~/.gemini/antigravity-cli/settings.json
  agy-statusline uninstall        Revert statusLine to empty default in settings.json
  agy-statusline doctor           Inspect installation, settings.json, git, and agy-swap integration
  agy-statusline demo [flags]     Preview the HUD with synthetic data (--width, --state, --theme, --all)
  agy-statusline themes           Preview all built-in color palettes
  agy-statusline version          Print version information

Install flags:
  --replace                       Replace agy's built-in bar (stack_with_default=false)
  --settings <path>               Custom settings.json path
  --bin <path>                    Custom executable path to record
`, version)
}
