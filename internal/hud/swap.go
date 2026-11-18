package hud

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ActiveFamily classifies the current model into "3p" (Claude/GPT) or "gemini".
func ActiveFamily(model *ModelInfo) string {
	if model == nil {
		return "gemini"
	}
	combined := strings.ToLower(model.ID + " " + model.DisplayName)
	for _, tok := range []string{"claude", "opus", "sonnet", "haiku", "gpt", "o1", "o3", "o4", "3p"} {
		if strings.Contains(combined, tok) {
			return "3p"
		}
	}
	return "gemini"
}

func secondaryFamily(family string) (string, string) {
	if family == "3p" {
		return "gemini", "Gem"
	}
	return "3p", "3P"
}

// ResolveAgySwapDir locates the agy-swap configuration directory.
func ResolveAgySwapDir(getenv func(string) string) string {
	if getenv != nil {
		if override := strings.TrimSpace(getenv("AGY_SWAP_DIR")); override != "" {
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
		return ""
	}
	return filepath.Join(home, ".gemini", "agy-swap")
}

func parseTierLabel(raw any) string {
	switch v := raw.(type) {
	case string:
		return normalizeTierText(v)
	case map[string]any:
		id, _ := v["id"].(string)
		name, _ := v["name"].(string)
		if t := normalizeTierText(id + " " + name); t != "" {
			return t
		}
	}
	return ""
}

func normalizeTierText(s string) string {
	low := strings.ToLower(strings.TrimSpace(s))
	if low == "" {
		return ""
	}
	if strings.Contains(low, "ultra") {
		return "ULTRA"
	}
	if strings.Contains(low, "pro") {
		return "PRO"
	}
	return ""
}

// LoadAgySwapInfo reads non-secret agy-swap metadata (config.json and accounts.json).
// It never reads vault.json or OS keychains.
func LoadAgySwapInfo(
	data *StatusLineData,
	family string,
	fleetCache map[string]fleetCacheEntry,
	now time.Time,
	getenv func(string) string,
) AgySwapInfo {
	var email, planTier string
	var hasQuota bool
	if data != nil {
		email = strings.TrimSpace(data.Email)
		planTier = data.PlanTier
		hasQuota = len(data.Quota) > 0
	}

	out := AgySwapInfo{
		ActiveTier: normalizeTierText(planTier),
	}
	dir := ResolveAgySwapDir(getenv)
	if dir == "" {
		if email != "" {
			out.Alias = emailLocalFallback(email)
		}
		return out
	}

	var cfg agySwapConfig
	if rawCfg, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		_ = json.Unmarshal(rawCfg, &cfg)
	}
	minPct := cfg.Policy.MinRemainingPct
	if minPct <= 0 {
		minPct = 10
	}

	var accounts map[string]agySwapAccount
	if rawAcc, err := os.ReadFile(filepath.Join(dir, "accounts.json")); err == nil {
		_ = json.Unmarshal(rawAcc, &accounts)
	}

	out.TotalAccounts = len(accounts)
	if email != "" {
		out.Alias = resolveAlias(email, cfg.Aliases, accounts)
	}

	if len(accounts) == 0 {
		return out
	}

	for key, acc := range accounts {
		accEmail := strings.TrimSpace(acc.Email)
		if accEmail == "" {
			accEmail = key
		}
		if email != "" && strings.EqualFold(accEmail, email) {
			if out.ActiveTier == "" && acc.QuotaSnapshot != nil {
				out.ActiveTier = parseTierLabel(acc.QuotaSnapshot.Tier)
			}
			if !hasQuota && acc.QuotaSnapshot != nil {
				out.FallbackQuota = snapshotToBuckets(acc.QuotaSnapshot, now)
			}
			continue
		}

		fv := buildFleetAccountView(accEmail, acc, cfg.Aliases, accounts, fleetCache, family, minPct, now)
		if fv.Status == "ready" {
			out.ReadyReserves++
		}
		out.Fleet = append(out.Fleet, fv)
	}

	sortFleetAccounts(out.Fleet)
	return out
}

func buildFleetAccountView(
	email string,
	acc agySwapAccount,
	aliases map[string]string,
	accounts map[string]agySwapAccount,
	fleetCache map[string]fleetCacheEntry,
	family string,
	minPct int,
	now time.Time,
) FleetAccountView {
	alias := resolveAlias(email, aliases, accounts)
	tier := ""
	var buckets map[string]QuotaBucket
	var snapObs time.Time
	hasSnapObs := false

	if acc.QuotaSnapshot != nil {
		tier = parseTierLabel(acc.QuotaSnapshot.Tier)
		buckets = snapshotToBuckets(acc.QuotaSnapshot, now)
		snapObs, hasSnapObs = parseTimestamp(acc.QuotaSnapshot.ObservedAt)
	}

	if cached, ok := fleetCache[strings.ToLower(strings.TrimSpace(email))]; ok && len(cached.Quota) > 0 {
		cacheObs, hasCacheObs := parseTimestamp(cached.ObservedAt)
		if !hasSnapObs || (hasCacheObs && cacheObs.After(snapObs)) {
			buckets = normalizeCachedBuckets(cached.Quota, now)
			if tier == "" && cached.Tier != "" {
				tier = cached.Tier
			}
		}
	}

	view := FleetAccountView{
		Email: email,
		Alias: alias,
		Tier:  tier,
	}

	if strings.TrimSpace(acc.SecretRef) == "" {
		view.Status = "unavailable"
		return view
	}

	secFam, _ := secondaryFamily(family)
	priCooldown := maxFamilyCooldown(acc.QuotaLimits, family, now)
	secCooldown := maxFamilyCooldown(acc.QuotaLimits, secFam, now)

	pri5h, hasPri5h := lookupBucket(buckets, family, "5h")
	pri7d, hasPri7d := lookupBucket(buckets, family, "weekly")
	priMin, hasPri := poolMinRemainingPct(pri5h, hasPri5h, pri7d, hasPri7d)
	priExhaustWait := minPoolResetWait(pri5h, hasPri5h, pri7d, hasPri7d, minPct, now)

	sec5h, hasSec5h := lookupBucket(buckets, secFam, "5h")
	sec7d, hasSec7d := lookupBucket(buckets, secFam, "weekly")
	secMin, hasSec := poolMinRemainingPct(sec5h, hasSec5h, sec7d, hasSec7d)

	view.PrimaryPct = priMin
	view.SecondaryPct = secMin
	view.HasSecondary = hasSec
	view.LowQuotaBlocked = hasPri && priMin < float64(minPct)

	if priCooldown == 0 && hasPri && priMin >= float64(minPct) {
		view.Status = "ready"
		return view
	}

	wait := priCooldown
	if priExhaustWait > wait {
		wait = priExhaustWait
	}
	view.WaitSeconds = wait

	if !hasPri && priCooldown == 0 {
		view.Status = "unavailable"
		return view
	}

	if secCooldown == 0 && hasSec && secMin >= float64(minPct) {
		view.Status = "partial"
		return view
	}

	view.Status = "cooldown"
	return view
}

func normalizeCachedBuckets(raw map[string]QuotaBucket, now time.Time) map[string]QuotaBucket {
	out := make(map[string]QuotaBucket, len(raw))
	for k, b := range raw {
		if b.Disabled || b.RemainingFraction == nil {
			out[k] = b
			continue
		}
		if b.ResetTime != nil {
			if ts, ok := parseTimestamp(*b.ResetTime); ok && !ts.After(now) {
				one := 1.0
				zero := int64(0)
				b.RemainingFraction = &one
				b.ResetInSeconds = &zero
			}
		}
		out[k] = b
	}
	return out
}

func maxFamilyCooldown(limits map[string]agySwapQuotaLimit, family string, now time.Time) int64 {
	var maxSec int64
	for _, lim := range limits {
		ts, ok := parseTimestamp(lim.ResetAt)
		if !ok || !ts.After(now) {
			continue
		}
		if limitMatchesFamily(lim.Family, family) {
			sec := int64(ts.Sub(now).Seconds())
			if sec > maxSec {
				maxSec = sec
			}
		}
	}
	return maxSec
}

func minPoolResetWait(b5h QuotaBucket, has5h bool, b7d QuotaBucket, has7d bool, minPct int, now time.Time) int64 {
	var maxWait int64
	for _, item := range []struct {
		b  QuotaBucket
		ok bool
	}{{b5h, has5h}, {b7d, has7d}} {
		if !item.ok {
			continue
		}
		if bucketRemainingPct(item.b) < float64(minPct) {
			if sec, ok := bucketResetSeconds(item.b, now); ok && sec > maxWait {
				maxWait = sec
			}
		}
	}
	return maxWait
}

func sortFleetAccounts(fleet []FleetAccountView) {
	statusRank := func(s string) int {
		switch s {
		case "ready":
			return 0
		case "partial":
			return 1
		case "cooldown":
			return 2
		default:
			return 3
		}
	}
	tierRank := func(t string) int {
		switch t {
		case "ULTRA":
			return 0
		case "PRO":
			return 1
		default:
			return 2
		}
	}

	sort.Slice(fleet, func(i, j int) bool {
		a, b := fleet[i], fleet[j]
		ra, rb := statusRank(a.Status), statusRank(b.Status)
		if ra != rb {
			return ra < rb
		}
		if a.Status == "ready" {
			pa, pb := int(math.Round(a.PrimaryPct)), int(math.Round(b.PrimaryPct))
			if pa != pb {
				return pa > pb
			}
			ta, tb := tierRank(a.Tier), tierRank(b.Tier)
			if ta != tb {
				return ta < tb
			}
		} else if a.Status == "partial" || a.Status == "cooldown" {
			if a.WaitSeconds != b.WaitSeconds {
				if a.WaitSeconds == 0 {
					return false
				}
				if b.WaitSeconds == 0 {
					return true
				}
				return a.WaitSeconds < b.WaitSeconds
			}
		}
		return strings.ToLower(a.Alias) < strings.ToLower(b.Alias)
	})
}

func resolveAlias(email string, aliases map[string]string, accounts map[string]agySwapAccount) string {
	if len(aliases) > 0 {
		keys := make([]string, 0, len(aliases))
		for k := range aliases {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := strings.TrimSpace(aliases[k])
			if strings.EqualFold(v, email) && strings.TrimSpace(k) != "" {
				return strings.TrimSpace(k)
			}
		}
		for _, k := range keys {
			if strings.EqualFold(k, email) && strings.TrimSpace(aliases[k]) != "" {
				return strings.TrimSpace(aliases[k])
			}
		}
	}
	for k, acc := range accounts {
		accEmail := strings.TrimSpace(acc.Email)
		if accEmail == "" {
			accEmail = k
		}
		if strings.EqualFold(accEmail, email) {
			if name := strings.TrimSpace(acc.Name); name != "" {
				return name
			}
		}
	}
	return emailLocalFallback(email)
}

func emailLocalFallback(email string) string {
	local := strings.TrimSpace(email)
	if at := strings.IndexByte(local, '@'); at > 0 {
		local = local[:at]
	}
	return clip(local, 10)
}

func snapshotToBuckets(snap *agySwapQuotaSnapshot, now time.Time) map[string]QuotaBucket {
	if snap == nil {
		return nil
	}
	out := make(map[string]QuotaBucket)
	for _, grp := range snap.Groups {
		for _, b := range grp.Buckets {
			id := strings.TrimSpace(b.ID)
			if id == "" {
				continue
			}
			frac := b.RemainingFraction
			var resetInPtr *int64
			var resetTimePtr *string
			if ts, ok := parseTimestamp(b.ResetAt); ok {
				iso := ts.UTC().Format(time.RFC3339)
				resetTimePtr = &iso
				if !ts.After(now) {
					frac = 1.0
					zero := int64(0)
					resetInPtr = &zero
				} else {
					sec := int64(ts.Sub(now).Seconds())
					resetInPtr = &sec
				}
			}
			f := clamp(frac, 0, 1)
			out[id] = QuotaBucket{
				RemainingFraction: &f,
				ResetTime:         resetTimePtr,
				ResetInSeconds:    resetInPtr,
			}
		}
	}
	return out
}

func limitMatchesFamily(limitFamily, activeFam string) bool {
	f := strings.ToLower(strings.TrimSpace(limitFamily))
	if f == "" {
		return true
	}
	if activeFam == "3p" {
		return strings.Contains(f, "3p") || strings.Contains(f, "claude") || strings.Contains(f, "third_party") || strings.Contains(f, "gpt")
	}
	return strings.Contains(f, "gemini")
}
