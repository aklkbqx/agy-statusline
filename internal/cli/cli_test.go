package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestInstallPreservesKeysAndRefusesCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	initial := `{
  "colorScheme": "tokyo night",
  "model": "Orchestrator (Carbon)",
  "permissions": {
    "allow": ["read_file(/workspace)"]
  },
  "statusLine": {
    "type": "",
    "command": "",
    "enabled": true
  },
  "trustedWorkspaces": ["/workspace"]
}
`
	if err := os.WriteFile(settingsPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(
		[]string{"install", "--settings", settingsPath, "--bin", "/usr/local/bin/agy-statusline"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		"0.1.0",
		"test",
		envMap(map[string]string{"HOME": dir, "NO_COLOR": "1"}),
	)
	if code != 0 {
		t.Fatalf("install failed (%d): %s", code, stderr.String())
	}

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("invalid json after install: %v", err)
	}
	if parsed["colorScheme"] != "tokyo night" || parsed["model"] != "Orchestrator (Carbon)" {
		t.Fatalf("lost unrelated keys: %+v", parsed)
	}
	sl := parsed["statusLine"].(map[string]any)
	if sl["type"] != "command" || sl["command"] != "/usr/local/bin/agy-statusline" || sl["stack_with_default"] != true {
		t.Fatalf("unexpected statusLine after install: %+v", sl)
	}

	// Uninstall reverts cleanly while keeping permissions/trustedWorkspaces
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"uninstall", "--settings", settingsPath}, strings.NewReader(""), &stdout, &stderr, "0.1.0", "test", envMap(map[string]string{"HOME": dir}))
	if code != 0 {
		t.Fatalf("uninstall failed (%d): %s", code, stderr.String())
	}
	raw, _ = os.ReadFile(settingsPath)
	_ = json.Unmarshal(raw, &parsed)
	sl = parsed["statusLine"].(map[string]any)
	if sl["type"] != "" || sl["command"] != "" {
		t.Fatalf("uninstall did not clear statusLine: %+v", sl)
	}
	if _, ok := parsed["trustedWorkspaces"]; !ok {
		t.Fatalf("uninstall dropped trustedWorkspaces")
	}

	// Corrupt JSON must be refused without overwriting
	corruptPath := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corruptPath, []byte("{bad json"), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	code = Run([]string{"install", "--settings", corruptPath, "--bin", "/usr/local/bin/agy-statusline"}, strings.NewReader(""), &stdout, &stderr, "0.1.0", "test", envMap(map[string]string{"HOME": dir}))
	if code == 0 {
		t.Fatalf("expected non-zero exit for malformed settings.json")
	}
	after, _ := os.ReadFile(corruptPath)
	if string(after) != "{bad json" {
		t.Fatalf("malformed file was overwritten: %q", string(after))
	}
}

func TestDoctorDemoThemesAndVersion(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	_ = os.WriteFile(settingsPath, []byte(`{"colorScheme":"nord"}`), 0o600)

	var stdout, stderr bytes.Buffer
	env := envMap(map[string]string{"HOME": dir, "NO_COLOR": "1", "AGY_SWAP_DIR": filepath.Join(dir, "no-swap")})

	if code := Run([]string{"version"}, strings.NewReader(""), &stdout, &stderr, "0.1.0", "abc", env); code != 0 || !strings.Contains(stdout.String(), "v0.1.0") {
		t.Fatalf("version failed: code=%d out=%q", code, stdout.String())
	}

	stdout.Reset()
	if code := Run([]string{"themes"}, strings.NewReader(""), &stdout, &stderr, "0.1.0", "abc", env); code != 0 || !strings.Contains(stdout.String(), "catppuccin") {
		t.Fatalf("themes failed: code=%d out=%q", code, stdout.String())
	}

	stdout.Reset()
	if code := Run([]string{"demo", "--all", "--state", "thinking"}, strings.NewReader(""), &stdout, &stderr, "0.1.0", "abc", env); code != 0 || !strings.Contains(stdout.String(), "Studio Ultra") || !strings.Contains(stdout.String(), "THINKING") {
		t.Fatalf("demo failed: code=%d out=%q", code, stdout.String())
	}

	stdout.Reset()
	if code := Run([]string{"doctor", "--settings", settingsPath}, strings.NewReader(""), &stdout, &stderr, "0.1.0", "abc", env); code != 0 || !strings.Contains(stdout.String(), "agy-statusline doctor") {
		t.Fatalf("doctor failed: code=%d out=%q err=%q", code, stdout.String(), stderr.String())
	}
}
