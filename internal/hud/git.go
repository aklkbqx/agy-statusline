package hud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	gitCacheTTL = 4 * time.Second
	gitTimeout  = 1200 * time.Millisecond
	revTimeout  = 500 * time.Millisecond
)

func loadGitStatus(data *StatusLineData, stateDir string, now time.Time, getenv func(string) string) GitDetail {
	var detail GitDetail
	if data.VCS != nil {
		detail.Branch = strings.TrimSpace(data.VCS.Branch)
		detail.Dirty = data.VCS.Dirty
	}

	dir := ""
	if data.Workspace != nil {
		dir = strings.TrimSpace(data.Workspace.CurrentDir)
	}
	if dir == "" {
		dir = strings.TrimSpace(data.CWD)
	}
	if dir == "" {
		return detail
	}

	home := ""
	if getenv != nil {
		home = strings.TrimSpace(getenv("HOME"))
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home != "" && filepath.Clean(dir) == filepath.Clean(home) {
		if _, err := os.Stat(filepath.Join(home, ".git")); err != nil {
			return detail
		}
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return detail
	}

	cacheDir := filepath.Join(stateDir, "git")
	sum := sha256.Sum256([]byte(filepath.Clean(dir)))
	cachePath := filepath.Join(cacheDir, hex.EncodeToString(sum[:])[:16]+".json")

	if st, err := os.Stat(cachePath); err == nil && now.Sub(st.ModTime()) < gitCacheTTL {
		if raw, err := os.ReadFile(cachePath); err == nil {
			var cached GitDetail
			if err := json.Unmarshal(raw, &cached); err == nil && cached.Probed {
				return cached
			}
		}
	}

	probed, ok := ProbeGitPorcelain(dir)
	if !ok {
		return detail
	}
	writeJSONAtomic(cacheDir, cachePath, probed)
	return probed
}

// ProbeGitPorcelain queries git status using porcelain v2 with optional locks disabled.
func ProbeGitPorcelain(dir string) (GitDetail, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "--no-optional-locks", "status", "--porcelain=v2", "--branch")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return GitDetail{}, false
	}

	detail := GitDetail{
		Probed:     true,
		IsWorktree: detectLinkedWorktree(dir),
	}
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			detail.Branch = strings.TrimSpace(strings.TrimPrefix(line, "# branch.head "))
		case strings.HasPrefix(line, "# branch.ab "):
			ab := strings.TrimPrefix(line, "# branch.ab ")
			for _, field := range strings.Fields(ab) {
				if strings.HasPrefix(field, "+") {
					detail.Ahead, _ = strconv.Atoi(field[1:])
				} else if strings.HasPrefix(field, "-") {
					detail.Behind, _ = strconv.Atoi(field[1:])
				}
			}
		case strings.HasPrefix(line, "1 ") || strings.HasPrefix(line, "2 "):
			if len(line) >= 4 {
				if line[2] != '.' {
					detail.Staged++
				}
				if line[3] != '.' {
					detail.Modified++
				}
			}
		case strings.HasPrefix(line, "u "):
			detail.Conflicts++
		case strings.HasPrefix(line, "? "):
			detail.Untracked++
		}
	}

	if detail.Branch == "(detached)" {
		rctx, rcancel := context.WithTimeout(context.Background(), revTimeout)
		defer rcancel()
		rcmd := exec.CommandContext(rctx, "git", "-C", dir, "--no-optional-locks", "rev-parse", "--short", "HEAD")
		rcmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
		if sha, err := rcmd.Output(); err == nil {
			s := strings.TrimSpace(string(sha))
			if s != "" {
				detail.Branch = "@" + s
			}
		}
	}

	detail.Dirty = detail.Staged+detail.Modified+detail.Untracked+detail.Conflicts > 0
	return detail, true
}

func detectLinkedWorktree(dir string) bool {
	cur := filepath.Clean(dir)
	for {
		dotGit := filepath.Join(cur, ".git")
		if st, err := os.Lstat(dotGit); err == nil {
			return st.Mode().IsRegular()
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		cur = parent
	}
}
