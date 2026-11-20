package hud

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	maxFleetCache            = 16
	maxSessionHist           = 32
	deltaShowSec             = 8
	fleetWriteHeartbeatSec   = 30
	sessionWriteHeartbeatSec = 30
	staleTempAge             = 60 * time.Second
)

var tempFileSeq atomic.Uint64

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

// ResolveStateDir returns the private cache directory for ephemeral non-secret HUD state.
func ResolveStateDir(getenv func(string) string) string {
	if getenv != nil {
		if dir := strings.TrimSpace(getenv("AGY_STATUSLINE_STATE_DIR")); dir != "" {
			return dir
		}
	}
	if cacheBase, err := os.UserCacheDir(); err == nil && strings.TrimSpace(cacheBase) != "" {
		return filepath.Join(cacheBase, "agy-statusline")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("agy-statusline-%d", os.Getuid()))
}

func isTempArtifactName(name string) bool {
	return strings.HasPrefix(name, "tmp-") || strings.Contains(name, ".tmp.")
}

func sweepStaleTemps(dir string, wallNow time.Time, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isTempArtifactName(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if wallNow.Sub(info.ModTime()) > maxAge {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// CountOrphanedTemps counts leftover temporary files in stateDir and its git/sessions subdirs.
func CountOrphanedTemps(stateDir string) int {
	total := 0
	for _, dir := range []string{stateDir, filepath.Join(stateDir, "git"), filepath.Join(stateDir, "sessions")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && isTempArtifactName(entry.Name()) {
				total++
			}
		}
	}
	return total
}

func writeJSONAtomic(dir, path string, value any) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	tmpName := filepath.Join(dir, fmt.Sprintf("%s.tmp.%d.%d", filepath.Base(path), os.Getpid(), tempFileSeq.Add(1)))
	tmp, err := os.OpenFile(tmpName, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	sweepStaleTemps(dir, time.Now(), staleTempAge)
}

func loadAndUpdateFleetCache(stateDir string, data *StatusLineData, now time.Time) map[string]fleetCacheEntry {
	cachePath := filepath.Join(stateDir, "fleet.json")
	cache := readFleetCache(cachePath)

	email := strings.ToLower(strings.TrimSpace(data.Email))
	if email == "" || len(data.Quota) == 0 {
		return cache
	}

	prev, exists := cache[email]
	tier := normalizeTierText(data.PlanTier)
	if tier == "" && exists {
		tier = prev.Tier
	}
	cloned := make(map[string]QuotaBucket, len(data.Quota))
	for k, b := range data.Quota {
		if (b.ResetTime == nil || strings.TrimSpace(*b.ResetTime) == "") && b.ResetInSeconds != nil && *b.ResetInSeconds > 0 {
			ts := now.Add(time.Duration(*b.ResetInSeconds) * time.Second).UTC().Format(time.RFC3339)
			b.ResetTime = &ts
		}
		// Never persist a frozen ResetInSeconds alongside an absolute ResetTime.
		if b.ResetTime != nil && strings.TrimSpace(*b.ResetTime) != "" {
			b.ResetInSeconds = nil
		}
		cloned[k] = b
	}
	next := fleetCacheEntry{
		ObservedAt: now.UTC().Format(time.RFC3339),
		UpdatedAt:  now.Unix(),
		Tier:       tier,
		Quota:      cloned,
	}

	if !fleetEntryNeedsWrite(prev, exists, next) {
		cache[email] = next
		return cache
	}

	lockPath := filepath.Join(stateDir, "fleet.lock")
	locked := withFileLock(lockPath, func() {
		merged := readFleetCache(cachePath)
		if mergedTier := next.Tier; mergedTier == "" {
			if existing, ok := merged[email]; ok {
				next.Tier = existing.Tier
			}
		}
		merged[email] = next
		trimFleetCache(merged, maxFleetCache)
		writeJSONAtomic(stateDir, cachePath, merged)
		cache = merged
	})
	if !locked {
		cache[email] = next
	}
	return cache
}

func readFleetCache(cachePath string) map[string]fleetCacheEntry {
	cache := make(map[string]fleetCacheEntry)
	if raw, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(raw, &cache)
	}
	return cache
}

func fleetEntryNeedsWrite(prev fleetCacheEntry, exists bool, next fleetCacheEntry) bool {
	if !exists {
		return true
	}
	if prev.Tier != next.Tier {
		return true
	}
	age := next.UpdatedAt - prev.UpdatedAt
	if age < 0 || age >= fleetWriteHeartbeatSec {
		return true
	}
	return quotaMapsDiffer(prev.Quota, next.Quota)
}

func quotaMapsDiffer(a, b map[string]QuotaBucket) bool {
	if len(a) != len(b) {
		return true
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return true
		}
		if va.Disabled != vb.Disabled {
			return true
		}
		if (va.RemainingFraction == nil) != (vb.RemainingFraction == nil) {
			return true
		}
		if va.RemainingFraction != nil && math.Abs(*va.RemainingFraction-*vb.RemainingFraction) > 1e-4 {
			return true
		}
		if resetTimesDiffer(va, vb) {
			return true
		}
	}
	return false
}

func resetTimesDiffer(a, b QuotaBucket) bool {
	aStr, bStr := "", ""
	if a.ResetTime != nil {
		aStr = strings.TrimSpace(*a.ResetTime)
	}
	if b.ResetTime != nil {
		bStr = strings.TrimSpace(*b.ResetTime)
	}
	if aStr == bStr {
		if aStr != "" {
			return false
		}
		if (a.ResetInSeconds == nil) != (b.ResetInSeconds == nil) {
			return true
		}
		if a.ResetInSeconds != nil && *a.ResetInSeconds != *b.ResetInSeconds {
			return true
		}
		return false
	}
	ta, oka := parseTimestamp(aStr)
	tb, okb := parseTimestamp(bStr)
	if !oka || !okb {
		return true
	}
	diff := ta.Sub(tb)
	if diff < 0 {
		diff = -diff
	}
	// Ignore <=5s jitter from ResetInSeconds rounding across consecutive polls.
	return diff > 5*time.Second
}

func trimFleetCache(cache map[string]fleetCacheEntry, maxEntries int) {
	if len(cache) <= maxEntries {
		return
	}
	type pair struct {
		k  string
		ts int64
	}
	items := make([]pair, 0, len(cache))
	for k, v := range cache {
		items = append(items, pair{k, v.UpdatedAt})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ts < items[j].ts })
	for len(items) > maxEntries {
		delete(cache, items[0].k)
		items = items[1:]
	}
}

func loadAndUpdateSessionState(stateDir string, data *StatusLineData, usedTokens int, now time.Time) SessionView {
	key := firstNonEmpty(strings.TrimSpace(data.ConversationID), strings.TrimSpace(data.SessionID), "default")
	sessDir := filepath.Join(stateDir, "sessions")
	sessPath := filepath.Join(sessDir, shortHash(key)+".json")

	var entry sessionStateEntry
	exists := false
	if raw, err := os.ReadFile(sessPath); err == nil {
		if err := json.Unmarshal(raw, &entry); err == nil && entry.StartedAt > 0 {
			exists = true
		}
	}
	if !exists {
		// One-time migration fallback from legacy monolithic session.json.
		if legacyRaw, err := os.ReadFile(filepath.Join(stateDir, "session.json")); err == nil {
			var legacy map[string]sessionStateEntry
			if err := json.Unmarshal(legacyRaw, &legacy); err == nil {
				if prev, ok := legacy[key]; ok && prev.StartedAt > 0 {
					entry = prev
				}
			}
		}
	}

	nowSec := now.Unix()
	dirty := !exists
	if entry.StartedAt <= 0 {
		entry.StartedAt = nowSec
		dirty = true
	}
	if usedTokens > 0 && entry.LastTokens > 0 && usedTokens-entry.LastTokens >= 500 {
		entry.DeltaTokens = usedTokens - entry.LastTokens
		entry.DeltaUntil = nowSec + deltaShowSec
		dirty = true
	}
	if usedTokens > 0 && usedTokens != entry.LastTokens {
		entry.LastTokens = usedTokens
		dirty = true
	}
	if entry.LastSeen <= 0 || nowSec-entry.LastSeen >= sessionWriteHeartbeatSec || nowSec < entry.LastSeen {
		entry.LastSeen = nowSec
		dirty = true
	}

	if dirty {
		writeJSONAtomic(sessDir, sessPath, entry)
		if !exists {
			pruneSessionFiles(sessDir, maxSessionHist)
		}
	}

	view := SessionView{
		ElapsedSec: maxInt64(0, nowSec-entry.StartedAt),
	}
	if nowSec <= entry.DeltaUntil && entry.DeltaTokens >= 500 {
		view.ActiveDeltaTokens = entry.DeltaTokens
	}
	return view
}

func pruneSessionFiles(sessDir string, maxFiles int) {
	entries, err := os.ReadDir(sessDir)
	if err != nil || len(entries) <= maxFiles {
		return
	}
	type fileAge struct {
		name string
		mod  time.Time
	}
	files := make([]fileAge, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, fileAge{name: entry.Name(), mod: info.ModTime()})
	}
	if len(files) <= maxFiles {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for i := 0; i < len(files)-maxFiles; i++ {
		_ = os.Remove(filepath.Join(sessDir, files[i].name))
	}
}
