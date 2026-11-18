package hud

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxFleetCache  = 16
	maxSessionHist = 20
	deltaShowSec   = 8
)

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

func writeJSONAtomic(dir, path string, value any) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, "tmp-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
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
	_ = os.Rename(tmpName, path)
}

func loadAndUpdateFleetCache(stateDir string, data *StatusLineData, now time.Time) map[string]fleetCacheEntry {
	cachePath := filepath.Join(stateDir, "fleet.json")
	cache := make(map[string]fleetCacheEntry)
	if raw, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(raw, &cache)
	}

	email := strings.ToLower(strings.TrimSpace(data.Email))
	if email != "" && len(data.Quota) > 0 {
		tier := normalizeTierText(data.PlanTier)
		if tier == "" {
			if prev, ok := cache[email]; ok {
				tier = prev.Tier
			}
		}
		cloned := make(map[string]QuotaBucket, len(data.Quota))
		for k, b := range data.Quota {
			if (b.ResetTime == nil || strings.TrimSpace(*b.ResetTime) == "") && b.ResetInSeconds != nil && *b.ResetInSeconds > 0 {
				ts := now.Add(time.Duration(*b.ResetInSeconds) * time.Second).UTC().Format(time.RFC3339)
				b.ResetTime = &ts
			}
			cloned[k] = b
		}
		cache[email] = fleetCacheEntry{
			ObservedAt: now.UTC().Format(time.RFC3339),
			UpdatedAt:  now.Unix(),
			Tier:       tier,
			Quota:      cloned,
		}
		trimFleetCache(cache, maxFleetCache)
		writeJSONAtomic(stateDir, cachePath, cache)
	}
	return cache
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
	cachePath := filepath.Join(stateDir, "session.json")
	hist := make(map[string]sessionStateEntry)
	if raw, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(raw, &hist)
	}

	key := firstNonEmpty(strings.TrimSpace(data.ConversationID), strings.TrimSpace(data.SessionID), "default")
	entry, ok := hist[key]
	if !ok || entry.StartedAt <= 0 {
		entry.StartedAt = now.Unix()
	}
	if usedTokens > 0 && entry.LastTokens > 0 && usedTokens-entry.LastTokens >= 500 {
		entry.DeltaTokens = usedTokens - entry.LastTokens
		entry.DeltaUntil = now.Unix() + deltaShowSec
	}
	if usedTokens > 0 {
		entry.LastTokens = usedTokens
	}
	entry.LastSeen = now.Unix()
	hist[key] = entry

	if len(hist) > maxSessionHist {
		type pair struct {
			k  string
			ts int64
		}
		items := make([]pair, 0, len(hist))
		for k, v := range hist {
			items = append(items, pair{k, v.LastSeen})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ts < items[j].ts })
		for len(items) > maxSessionHist {
			delete(hist, items[0].k)
			items = items[1:]
		}
	}
	writeJSONAtomic(stateDir, cachePath, hist)

	view := SessionView{
		ElapsedSec: maxInt64(0, now.Unix()-entry.StartedAt),
	}
	if now.Unix() <= entry.DeltaUntil && entry.DeltaTokens >= 500 {
		view.ActiveDeltaTokens = entry.DeltaTokens
	}
	return view
}
