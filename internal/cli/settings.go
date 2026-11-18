package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	Path        string
	Exists      bool
	Valid       bool
	ColorScheme string
	StatusLine  StatusLineSettings
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

// InstallSettings updates only the "statusLine" block in settingsPath while preserving all other keys.
func InstallSettings(settingsPath, binPath string, stackWithDefault bool) error {
	if strings.TrimSpace(settingsPath) == "" {
		return errors.New("settings path cannot be empty")
	}
	if strings.TrimSpace(binPath) == "" {
		return errors.New("binary path cannot be empty")
	}

	root, err := readSettingsMap(settingsPath, true)
	if err != nil {
		return err
	}

	enabled := true
	sl := StatusLineSettings{
		Type:             "command",
		Command:          binPath,
		Enabled:          &enabled,
		StackWithDefault: stackWithDefault,
	}
	rawSL, err := json.Marshal(sl)
	if err != nil {
		return err
	}
	root["statusLine"] = rawSL
	return writeSettingsMapAtomic(settingsPath, root)
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
	return true, writeSettingsMapAtomic(settingsPath, root)
}

// InspectSettings reads settingsPath without modifying it.
func InspectSettings(settingsPath string) SettingsReport {
	rep := SettingsReport{Path: settingsPath}
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
