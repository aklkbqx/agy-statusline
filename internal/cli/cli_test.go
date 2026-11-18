package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	wantCmd := BuildStatusLineCommand("/usr/local/bin/agy-statusline", runtime.GOOS)
	if sl["type"] != "command" || sl["command"] != wantCmd || sl["stack_with_default"] != true {
		t.Fatalf("unexpected statusLine after install: %+v (want command=%q)", sl, wantCmd)
	}
	if !IsFailOpenCommand(wantCmd) {
		t.Fatalf("expected fail-open command, got %q", wantCmd)
	}

	// Raw mode writes the bare binary path when explicitly requested
	stdout.Reset()
	stderr.Reset()
	code = Run(
		[]string{"install", "--raw-command", "--settings", settingsPath, "--bin", "/usr/local/bin/agy-statusline"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		"0.1.0",
		"test",
		envMap(map[string]string{"HOME": dir, "NO_COLOR": "1"}),
	)
	if code != 0 {
		t.Fatalf("raw install failed (%d): %s", code, stderr.String())
	}
	raw, _ = os.ReadFile(settingsPath)
	_ = json.Unmarshal(raw, &parsed)
	sl = parsed["statusLine"].(map[string]any)
	if sl["command"] != "/usr/local/bin/agy-statusline" {
		t.Fatalf("expected bare binary path with --raw-command, got %+v", sl)
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

func TestFailOpenGuardNeverExits127WhenBinaryMissing(t *testing.T) {
	if winCmd := BuildStatusLineCommand(`C:\bin\agy-statusline.exe`, "windows"); !IsFailOpenCommand(winCmd) {
		t.Fatalf("expected windows guard to be fail-open, got %q", winCmd)
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX /bin/sh check only runs on Unix")
	}

	missingBin := filepath.Join(t.TempDir(), "does not exist's", "agy-statusline")
	cmdStr := BuildStatusLineCommand(missingBin, "darwin")
	if !IsFailOpenCommand(cmdStr) {
		t.Fatalf("expected fail-open command, got %q", cmdStr)
	}

	var stdout, stderr bytes.Buffer
	proc := exec.Command("/bin/sh", "-c", cmdStr)
	proc.Env = []string{"PATH="}
	proc.Stdin = strings.NewReader(`{"model":"gemini"}`)
	proc.Stdout = &stdout
	proc.Stderr = &stderr
	if err := proc.Run(); err != nil {
		t.Fatalf("fail-open command must exit 0 when binary missing, got err=%v stderr=%q", err, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("expected empty output on missing binary, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestFailOpenGuardExecutesBinaryAndPassesStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX /bin/sh check only runs on Unix")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "agy's statusline")
	script := "#!/bin/sh\nIFS= read -r line || true\nprintf '%s' \"$line\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cmdStr := BuildStatusLineCommand(bin, "darwin")
	var stdout, stderr bytes.Buffer
	proc := exec.Command("/bin/sh", "-c", cmdStr)
	proc.Env = []string{"PATH="}
	proc.Stdin = strings.NewReader(`{"model":"gemini"}`)
	proc.Stdout = &stdout
	proc.Stderr = &stderr
	if err := proc.Run(); err != nil {
		t.Fatalf("guarded command failed: %v stderr=%q", err, stderr.String())
	}
	if stdout.String() != `{"model":"gemini"}` {
		t.Fatalf("stdin not passed cleanly: got %q", stdout.String())
	}
}

func TestCompatShimLifecyclePreservesSiblingAgentAPI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink shim lifecycle runs on Unix")
	}

	root := t.TempDir()
	binDir := filepath.Join(root, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realBin := filepath.Join(binDir, "agy-statusline")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	agyDir := filepath.Join(root, ".gemini", "antigravity-cli")
	legacyBinDir := filepath.Join(agyDir, "bin")
	if err := os.MkdirAll(legacyBinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentAPI := filepath.Join(legacyBinDir, "agentapi")
	if err := os.WriteFile(agentAPI, []byte("#!/bin/sh\necho api\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	settingsPath := filepath.Join(agyDir, "settings.json")
	var stdout, stderr bytes.Buffer
	env := envMap(map[string]string{"HOME": root, "NO_COLOR": "1"})

	if code := Run([]string{"install", "--settings", settingsPath, "--bin", realBin}, strings.NewReader(""), &stdout, &stderr, "0.1.1", "test", env); code != 0 {
		t.Fatalf("install failed (%d): %s", code, stderr.String())
	}

	shim := filepath.Join(legacyBinDir, "agy-statusline")
	target, err := os.Readlink(shim)
	if err != nil {
		t.Fatalf("expected compat symlink at %s: %v", shim, err)
	}
	if target != realBin {
		t.Fatalf("unexpected symlink target: got %q want %q", target, realBin)
	}
	if _, err := os.Stat(agentAPI); err != nil {
		t.Fatalf("sibling agentapi should be preserved: %v", err)
	}

	rep := InspectSettings(settingsPath)
	if rep.CompatShimPath != shim || rep.CompatShimTarget != realBin || !rep.FailOpenGuard {
		t.Fatalf("unexpected inspect report: %+v", rep)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"uninstall", "--settings", settingsPath}, strings.NewReader(""), &stdout, &stderr, "0.1.1", "test", env); code != 0 {
		t.Fatalf("uninstall failed (%d): %s", code, stderr.String())
	}
	if _, err := os.Lstat(shim); !os.IsNotExist(err) {
		t.Fatalf("expected compat symlink %s to be removed on uninstall, err=%v", shim, err)
	}
	if _, err := os.Stat(agentAPI); err != nil {
		t.Fatalf("sibling agentapi must remain intact after uninstall: %v", err)
	}
}
