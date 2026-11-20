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
	"time"
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

func TestRefreshGitSubcommandAndDoctorFreshness(t *testing.T) {
	stateDir := t.TempDir()
	swapDir := t.TempDir()
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
	_ = os.WriteFile(filepath.Join(repo, "x.txt"), []byte("hello\n"), 0o644)
	runGit("add", "x.txt")
	runGit("commit", "-m", "init")

	var stdout, stderr bytes.Buffer
	env := envMap(map[string]string{
		"HOME":                     t.TempDir(),
		"NO_COLOR":                 "1",
		"AGY_STATUSLINE_STATE_DIR": stateDir,
		"AGY_SWAP_DIR":             swapDir,
	})

	if code := Run([]string{"_refresh-git", stateDir, repo}, strings.NewReader(""), &stdout, &stderr, "0.1.2", "test", env); code != 0 {
		t.Fatalf("_refresh-git failed (%d): %s", code, stderr.String())
	}

	gitEntries, err := os.ReadDir(filepath.Join(stateDir, "git"))
	if err != nil {
		t.Fatalf("expected git cache dir: %v", err)
	}
	foundJSON := false
	for _, e := range gitEntries {
		if strings.HasSuffix(e.Name(), ".json") {
			foundJSON = true
		}
	}
	if !foundJSON {
		t.Fatalf("expected _refresh-git to write git cache json, entries=%v", gitEntries)
	}

	// Write a stale agy-swap snapshot (3 hours old) and an orphaned tmp file -> doctor reports both.
	staleObs := time.Now().UTC().Add(-3 * time.Hour).Format(time.RFC3339)
	futureReset := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	_ = os.WriteFile(filepath.Join(swapDir, "config.json"), []byte(`{"policy":{"min_remaining_pct":10}}`), 0o600)
	accounts := map[string]any{
		"a@example.com": map[string]any{
			"email": "a@example.com", "name": "Alpha", "secret_ref": "v1",
			"quota_snapshot": map[string]any{
				"observed_at": staleObs,
				"groups": []map[string]any{{
					"id": "gemini", "buckets": []map[string]any{
						{"id": "gemini-5h", "remaining_fraction": 1.0, "reset_at": futureReset, "window": "5h"},
					},
				}},
			},
		},
	}
	accBytes, _ := json.Marshal(accounts)
	_ = os.WriteFile(filepath.Join(swapDir, "accounts.json"), accBytes, 0o600)
	_ = os.WriteFile(filepath.Join(stateDir, "tmp-orphan"), []byte(""), 0o600)

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"doctor"}, strings.NewReader(""), &stdout, &stderr, "0.1.2", "test", env); code != 0 {
		t.Fatalf("doctor failed (%d): %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "1 orphaned temp files") {
		t.Fatalf("expected doctor to report orphaned temp files, got:\n%s", out)
	}
	if !strings.Contains(out, "1 stale") || !strings.Contains(out, "agy-swap watch") {
		t.Fatalf("expected doctor to warn about stale agy-swap snapshots, got:\n%s", out)
	}
}

func TestBinaryAsyncGitRefreshDoesNotHangStdoutPipe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary build test in short mode")
	}
	binName := "agy-statusline"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(t.TempDir(), binName)
	build := exec.Command("go", "build", "-o", binPath, "../../cmd/agy-statusline")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

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
	_ = os.WriteFile(filepath.Join(repo, "f.txt"), []byte("v1\n"), 0o644)
	runGit("add", "f.txt")
	runGit("commit", "-m", "init")

	payload, _ := json.Marshal(map[string]any{
		"cwd":             repo,
		"conversation_id": "pipe-test",
		"terminal_width":  120,
		"model":           map[string]any{"id": "gemini-4-neon"},
		"workspace":       map[string]any{"current_dir": repo, "project_dir": repo},
		"context_window":  map[string]any{"context_window_size": 1000000, "used_percentage": 10},
	})
	baseEnv := append(os.Environ(),
		"NO_COLOR=1",
		"AGY_STATUSLINE_STATE_DIR="+stateDir,
		"AGY_SWAP_DIR="+filepath.Join(stateDir, "no-swap"),
	)

	// 1) Initial cold render populates cache on main.
	cmd1 := exec.Command(binPath)
	cmd1.Env = baseEnv
	cmd1.Stdin = bytes.NewReader(payload)
	out1, err := cmd1.Output()
	if err != nil {
		t.Fatalf("initial binary render failed: %v", err)
	}
	if !strings.Contains(string(out1), "⎇ main") {
		t.Fatalf("expected ⎇ main in initial output, got %q", string(out1))
	}

	// 2) Switch branch & dirty a file -> next render must return fast (no pipe hang) with new branch AND trigger async refresh.
	runGit("checkout", "-b", "feat/swr")
	_ = os.WriteFile(filepath.Join(repo, "f.txt"), []byte("v2\n"), 0o644)

	start := time.Now()
	cmd2 := exec.Command(binPath)
	cmd2.Env = baseEnv
	cmd2.Stdin = bytes.NewReader(payload)
	out2, err := cmd2.Output()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("second binary render failed: %v", err)
	}
	if !strings.Contains(string(out2), "⎇ feat/swr") {
		t.Fatalf("expected instant ⎇ feat/swr on frame 0, got %q", string(out2))
	}
	if elapsed > 2*time.Second {
		t.Fatalf("stdout pipe appeared blocked by background child: elapsed=%v", elapsed)
	}

	// 3) Wait briefly for detached _refresh-git to populate ~1 modified count in cache.
	deadline := time.Now().Add(2 * time.Second)
	sawModified := false
	for time.Now().Before(deadline) {
		cmd3 := exec.Command(binPath)
		cmd3.Env = baseEnv
		cmd3.Stdin = bytes.NewReader(payload)
		if out3, err := cmd3.Output(); err == nil && strings.Contains(string(out3), "~1") {
			sawModified = true
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if !sawModified {
		t.Fatalf("expected background _refresh-git to refresh modified count (~1) in cache")
	}
}
