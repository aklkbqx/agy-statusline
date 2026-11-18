package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type StatusLineSettings struct {
	Type             string `json:"type"`
	Command          string `json:"command"`
	Enabled          *bool  `json:"enabled,omitempty"`
	Padding          int    `json:"padding,omitempty"`
	StackWithDefault bool   `json:"stack_with_default,omitempty"`
}

type SettingsReport struct {
	Path             string
	Exists           bool
	Valid            bool
	ColorScheme      string
	StatusLine       StatusLineSettings
	FailOpenGuard    bool
	CompatShimPath   string
	CompatShimTarget string
}

// BuildStatusLineCommand wraps binPath in a POSIX/Windows guard so missing or
// moving binaries exit 0 cleanly instead of tripping agy's exit-127 banner and
// 30-failure auto-disable.
func BuildStatusLineCommand(binPath, goos string) string {
	binPath = strings.TrimSpace(binPath)
	if binPath == "" {
		return ""
	}
	if goos == "windows" {
		return fmt.Sprintf("if exist %q (%q) else (exit /b 0)", binPath, binPath)
	}
	sq := "'" + strings.ReplaceAll(binPath, "'", `'\''`) + "'"
	return fmt.Sprintf(
		"if [ -x %s ]; then exec %s; elif command -v agy-statusline >/dev/null 2>&1; then exec agy-statusline; else exit 0; fi",
		sq, sq,
	)
}

// IsFailOpenCommand reports whether cmd includes a zero-exit fallback when missing.
func IsFailOpenCommand(cmd string) bool {
	return strings.Contains(cmd, "else exit 0") ||
		strings.Contains(cmd, "|| exit 0") ||
		strings.Contains(cmd, "|| true") ||
		strings.Contains(cmd, "exit /b 0")
}

// CompatShimPath returns the legacy ~/.gemini/antigravity-cli/bin/agy-statusline
// symlink path when settingsPath lives in an antigravity-cli directory.
func CompatShimPath(settingsPath, goos string) string {
	if strings.TrimSpace(settingsPath) == "" {
		return ""
	}
	dir := filepath.Dir(filepath.Clean(settingsPath))
	if filepath.Base(dir) != "antigravity-cli" {
		return ""
	}
	name := "agy-statusline"
	if goos == "windows" {
		name = "agy-statusline.exe"
	}
	return filepath.Join(dir, "bin", name)
}

// EnsureCompatShim atomically creates or updates the compatibility symlink next
// to settingsPath so long-lived agy processes still caching the legacy path can
// execute agy-statusline cleanly without restarting.
func EnsureCompatShim(settingsPath, binPath, goos string) (string, error) {
	if goos == "windows" {
		return "", nil
	}
	binPath = strings.TrimSpace(binPath)
	if binPath == "" {
		return "", nil
	}
	shim := CompatShimPath(settingsPath, goos)
	if shim == "" || filepath.Clean(shim) == filepath.Clean(binPath) {
		return "", nil
	}
	st, err := os.Stat(binPath)
	if err != nil || st.IsDir() {
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(shim), 0o755); err != nil {
		return "", err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", shim, os.Getpid())
	_ = os.Remove(tmp)
	if err := os.Symlink(binPath, tmp); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, shim); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return shim, nil
}

func removeCompatShim(settingsPath, goos string) {
	shim := CompatShimPath(settingsPath, goos)
	if shim == "" {
		return
	}
	if fi, err := os.Lstat(shim); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		_ = os.Remove(shim)
	}
}

// DefaultSettingsPath returns the path to Antigravity CLI's settings.json.
func DefaultSettingsPath(getenv func(string) string) string {
	if getenv != nil {
		if override := strings.TrimSpace(getenv("AGY_SETTINGS_PATH")); override != "" {
			return override
		}
	}
	home := ""
	if getenv != nil {
		home = strings.TrimSpace(getenv("HOME"))
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home == "" {
		return filepath.Join(".gemini", "antigravity-cli", "settings.json")
	}
	return filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
}

// InstallSettings updates only the "statusLine" block in settingsPath with the
// fail-open shell guard while preserving all other keys.
func InstallSettings(settingsPath, binPath string, stackWithDefault bool) error {
	return InstallSettingsWithOptions(settingsPath, binPath, stackWithDefault, false, runtime.GOOS)
}

// InstallSettingsWithOptions updates only the "statusLine" block in settingsPath
// while preserving all other keys, optionally wrapping command with a fail-open guard.
func InstallSettingsWithOptions(settingsPath, binPath string, stackWithDefault, rawCommand bool, goos string) error {
	if strings.TrimSpace(settingsPath) == "" {
		return errors.New("settings path cannot be empty")
	}
	cleanBin := strings.TrimSpace(binPath)
	if cleanBin == "" {
		return errors.New("binary path cannot be empty")
	}

	root, err := readSettingsMap(settingsPath, true)
	if err != nil {
		return err
	}

	cmd := cleanBin
	if !rawCommand {
		cmd = BuildStatusLineCommand(cleanBin, goos)
	}

	enabled := true
	sl := StatusLineSettings{
		Type:             "command",
		Command:          cmd,
		Enabled:          &enabled,
		StackWithDefault: stackWithDefault,
	}
	rawSL, err := json.Marshal(sl)
	if err != nil {
		return err
	}
	root["statusLine"] = rawSL
	if err := writeSettingsMapAtomic(settingsPath, root); err != nil {
		return err
	}
	_, _ = EnsureCompatShim(settingsPath, cleanBin, goos)
	return nil
}

// UninstallSettings resets only the "statusLine" block in settingsPath while preserving all other keys.
func UninstallSettings(settingsPath string) (bool, error) {
	if strings.TrimSpace(settingsPath) == "" {
		return false, errors.New("settings path cannot be empty")
	}
	if _, err := os.Stat(settingsPath); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	root, err := readSettingsMap(settingsPath, false)
	if err != nil {
		return false, err
	}

	enabled := true
	sl := StatusLineSettings{
		Type:    "",
		Command: "",
		Enabled: &enabled,
	}
	rawSL, err := json.Marshal(sl)
	if err != nil {
		return false, err
	}
	root["statusLine"] = rawSL
	if err := writeSettingsMapAtomic(settingsPath, root); err != nil {
		return false, err
	}
	removeCompatShim(settingsPath, runtime.GOOS)
	return true, nil
}

// InspectSettings reads settingsPath without modifying it.
func InspectSettings(settingsPath string) SettingsReport {
	rep := SettingsReport{Path: settingsPath}
	if shim := CompatShimPath(settingsPath, runtime.GOOS); shim != "" {
		if fi, err := os.Lstat(shim); err == nil {
			rep.CompatShimPath = shim
			if fi.Mode()&os.ModeSymlink != 0 {
				if target, rerr := os.Readlink(shim); rerr == nil {
					rep.CompatShimTarget = target
				}
			}
		}
	}
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		return rep
	}
	rep.Exists = true
	if len(bytes.TrimSpace(raw)) == 0 {
		rep.Valid = true
		return rep
	}
	var doc struct {
		ColorScheme string             `json:"colorScheme"`
		StatusLine  StatusLineSettings `json:"statusLine"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return rep
	}
	rep.Valid = true
	rep.ColorScheme = doc.ColorScheme
	rep.StatusLine = doc.StatusLine
	rep.FailOpenGuard = IsFailOpenCommand(doc.StatusLine.Command)
	return rep
}

func readSettingsMap(settingsPath string, allowMissing bool) (map[string]json.RawMessage, error) {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && allowMissing {
			return make(map[string]json.RawMessage), nil
		}
		return nil, fmt.Errorf("cannot read %s: %w", settingsPath, err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return make(map[string]json.RawMessage), nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("refusing to overwrite malformed JSON in %s: %w", settingsPath, err)
	}
	if root == nil {
		root = make(map[string]json.RawMessage)
	}
	return root, nil
}

func writeSettingsMapAtomic(settingsPath string, root map[string]json.RawMessage) error {
	dir := filepath.Dir(settingsPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create settings dir %s: %w", dir, err)
	}
	formatted, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	formatted = append(formatted, '\n')

	tmp, err := os.CreateTemp(dir, "settings-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if _, err := tmp.Write(formatted); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, settingsPath)
}
