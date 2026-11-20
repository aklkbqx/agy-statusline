package hud

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	gitCacheTTL     = 2 * time.Second
	coldGitBudget   = 120 * time.Millisecond
	gitTimeout      = 1500 * time.Millisecond
	revTimeout      = 500 * time.Millisecond
	gitLockStaleAge = 5 * time.Second
)

// gitAsyncRefresher is invoked on stale/cold git cache misses; overridden in unit tests.
var gitAsyncRefresher = defaultAsyncGitRefresh

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

	gitDir, commonDir, isWorktree, isRepo := resolveGitDir(dir)
	if !isRepo {
		return detail
	}

	headBranch, refRelPath := readHeadInfo(gitDir)
	fp := gitFingerprint(gitDir, commonDir, refRelPath)

	cleanDir := filepath.Clean(dir)
	cacheDir := filepath.Join(stateDir, "git")
	cachePath := filepath.Join(cacheDir, shortHash(cleanDir)+".json")

	syncMode := getenv != nil && isTruthy(getenv("AGY_STATUSLINE_SYNC_GIT"))
	cached, hasCached := readGitCache(cachePath)
	if hasCached {
		probedAt := time.Unix(0, cached.ProbedAt)
		ttlFresh := cached.ProbedAt > 0 && now.Sub(probedAt) >= 0 && now.Sub(probedAt) < gitCacheTTL
		fpFresh := cached.Fingerprint != "" && cached.Fingerprint == fp
		dirtyTurnedOn := data.VCS != nil && data.VCS.Dirty && !cached.Detail.Dirty
		if ttlFresh && fpFresh && !dirtyTurnedOn {
			return cached.Detail
		}
		if !syncMode {
			// Stale-while-revalidate: never block the render hot path on git status.
			res := cached.Detail
			res.IsWorktree = isWorktree
			if !fpFresh && headBranch != "" {
				// Reflect branch switches immediately on frame 0 via .git/HEAD.
				res.Branch = headBranch
			}
			if dirtyTurnedOn {
				res.Dirty = true
			}
			gitAsyncRefresher(stateDir, cleanDir)
			return res
		}
	}

	budget := coldGitBudget
	if syncMode {
		budget = gitTimeout
	}
	probed, ok := probeGitPorcelainWithTimeout(cleanDir, budget)
	if ok {
		probed.IsWorktree = isWorktree
		writeJSONAtomic(cacheDir, cachePath, gitCacheEntry{
			Detail:      probed,
			Fingerprint: fp,
			ProbedAt:    now.UnixNano(),
		})
		return probed
	}

	// Cold probe exceeded the fast budget (e.g. huge monorepo): return instant HEAD info
	// and let the detached background helper populate the cache for the next render.
	if headBranch != "" {
		detail.Branch = headBranch
	}
	detail.IsWorktree = isWorktree
	if !syncMode {
		gitAsyncRefresher(stateDir, cleanDir)
	}
	return detail
}

// RefreshGitCacheLocked probes dir with the full git timeout and writes the git cache
// atomically, deduplicated via a per-repo lock file.
func RefreshGitCacheLocked(stateDir, dir string, now time.Time) bool {
	stateDir = strings.TrimSpace(stateDir)
	dir = strings.TrimSpace(dir)
	if stateDir == "" || dir == "" {
		return false
	}
	cleanDir := filepath.Clean(dir)
	cacheDir := filepath.Join(stateDir, "git")
	key := shortHash(cleanDir)
	lockPath := filepath.Join(cacheDir, key+".lock")

	release, ok := tryAcquireLock(lockPath, gitLockStaleAge)
	if !ok {
		return false
	}
	defer release()

	gitDir, commonDir, isWorktree, isRepo := resolveGitDir(cleanDir)
	if !isRepo {
		return false
	}
	// Capture fingerprint BEFORE probing so concurrent git mutations during the probe
	// still invalidate on the next render.
	_, refRelPath := readHeadInfo(gitDir)
	fp := gitFingerprint(gitDir, commonDir, refRelPath)

	probed, ok := probeGitPorcelainWithTimeout(cleanDir, gitTimeout)
	if !ok {
		return false
	}
	probed.IsWorktree = isWorktree
	cachePath := filepath.Join(cacheDir, key+".json")
	writeJSONAtomic(cacheDir, cachePath, gitCacheEntry{
		Detail:      probed,
		Fingerprint: fp,
		ProbedAt:    now.UnixNano(),
	})
	return true
}

func defaultAsyncGitRefresh(stateDir, dir string) {
	if stateDir == "" || dir == "" {
		return
	}
	lockPath := filepath.Join(stateDir, "git", shortHash(filepath.Clean(dir))+".lock")
	if isLockHeld(lockPath, gitLockStaleAge) {
		return
	}
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return
	}
	base := filepath.Base(exe)
	if strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe") {
		return
	}
	cmd := exec.Command(exe, "_refresh-git", stateDir, dir)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	detachCmd(cmd)
	_ = cmd.Start()
}

func readGitCache(cachePath string) (gitCacheEntry, bool) {
	st, err := os.Stat(cachePath)
	if err != nil {
		return gitCacheEntry{}, false
	}
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		return gitCacheEntry{}, false
	}
	var entry gitCacheEntry
	if err := json.Unmarshal(raw, &entry); err == nil && entry.Detail.Probed {
		if entry.ProbedAt <= 0 {
			entry.ProbedAt = st.ModTime().UnixNano()
		}
		return entry, true
	}
	// Legacy format fallback: raw GitDetail JSON without wrapper.
	var legacy GitDetail
	if err := json.Unmarshal(raw, &legacy); err == nil && legacy.Probed {
		return gitCacheEntry{
			Detail:   legacy,
			ProbedAt: st.ModTime().UnixNano(),
		}, true
	}
	return gitCacheEntry{}, false
}

// ProbeGitPorcelain queries git status using porcelain v2 with optional locks disabled.
func ProbeGitPorcelain(dir string) (GitDetail, bool) {
	return probeGitPorcelainWithTimeout(dir, gitTimeout)
}

func probeGitPorcelainWithTimeout(dir string, timeout time.Duration) (GitDetail, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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

func resolveGitDir(dir string) (gitDir, commonDir string, isWorktree, ok bool) {
	cur := filepath.Clean(dir)
	for {
		dotGit := filepath.Join(cur, ".git")
		if st, err := os.Lstat(dotGit); err == nil {
			if st.IsDir() {
				return dotGit, dotGit, false, true
			}
			if st.Mode().IsRegular() {
				raw, err := readSmallFile(dotGit, 512)
				if err != nil {
					return "", "", false, false
				}
				line := strings.TrimSpace(raw)
				if !strings.HasPrefix(strings.ToLower(line), "gitdir:") {
					return "", "", false, false
				}
				target := strings.TrimSpace(line[len("gitdir:"):])
				if target == "" {
					return "", "", false, false
				}
				if !filepath.IsAbs(target) {
					target = filepath.Clean(filepath.Join(cur, target))
				}
				common := target
				if cRaw, cErr := readSmallFile(filepath.Join(target, "commondir"), 512); cErr == nil {
					cRel := strings.TrimSpace(cRaw)
					if cRel != "" {
						if filepath.IsAbs(cRel) {
							common = filepath.Clean(cRel)
						} else {
							common = filepath.Clean(filepath.Join(target, cRel))
						}
					}
				}
				return target, common, true, true
			}
			return "", "", false, false
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", "", false, false
		}
		cur = parent
	}
}

func readHeadInfo(gitDir string) (branch, refRelPath string) {
	raw, err := readSmallFile(filepath.Join(gitDir, "HEAD"), 512)
	if err != nil {
		return "", ""
	}
	head := strings.TrimSpace(raw)
	if head == "" {
		return "", ""
	}
	if strings.HasPrefix(head, "ref:") {
		refRelPath = strings.TrimSpace(strings.TrimPrefix(head, "ref:"))
		switch {
		case strings.HasPrefix(refRelPath, "refs/heads/"):
			return strings.TrimPrefix(refRelPath, "refs/heads/"), refRelPath
		case strings.HasPrefix(refRelPath, "refs/tags/"):
			return strings.TrimPrefix(refRelPath, "refs/tags/"), refRelPath
		default:
			return refRelPath, refRelPath
		}
	}
	if len(head) >= 7 {
		return "@" + head[:7], ""
	}
	return "@" + head, ""
}

func gitFingerprint(gitDir, commonDir, refRelPath string) string {
	paths := []string{
		filepath.Join(gitDir, "HEAD"),
		filepath.Join(gitDir, "index"),
		filepath.Join(commonDir, "packed-refs"),
		filepath.Join(commonDir, "FETCH_HEAD"),
		filepath.Join(gitDir, "MERGE_HEAD"),
		filepath.Join(gitDir, "REBASE_HEAD"),
	}
	if refRelPath != "" {
		paths = append(paths, filepath.Join(commonDir, filepath.FromSlash(refRelPath)))
	}
	var b strings.Builder
	for i, p := range paths {
		if i > 0 {
			b.WriteByte('|')
		}
		if st, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%d:%d", st.ModTime().UnixNano(), st.Size())
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func readSmallFile(path string, maxBytes int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, maxBytes)
	n, err := f.Read(buf)
	if n == 0 && err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}

func detectLinkedWorktree(dir string) bool {
	_, _, isWorktree, ok := resolveGitDir(dir)
	return ok && isWorktree
}
