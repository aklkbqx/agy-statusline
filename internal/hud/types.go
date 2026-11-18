package hud

// StatusLineData mirrors the JSON payload piped on stdin by Google Antigravity CLI (agy).
type StatusLineData struct {
	CWD                     string                 `json:"cwd"`
	SessionID               string                 `json:"session_id"`
	ConversationID          string                 `json:"conversation_id"`
	ConversationTitle       string                 `json:"conversation_title,omitempty"`
	TranscriptPath          string                 `json:"transcript_path,omitempty"`
	Model                   *ModelInfo             `json:"model"`
	Workspace               *WorkspaceInfo         `json:"workspace"`
	Version                 string                 `json:"version"`
	ContextWindow           *ContextInfo           `json:"context_window"`
	Exceeds200kTokens       *bool                  `json:"exceeds_200k_tokens"`
	Quota                   map[string]QuotaBucket `json:"quota,omitempty"`
	AgentState              string                 `json:"agent_state,omitempty"`
	VCS                     *VCSInfo               `json:"vcs,omitempty"`
	CycleMode               string                 `json:"cycle_mode,omitempty"`
	Sandbox                 *SandboxInfo           `json:"sandbox,omitempty"`
	Subagents               []SubagentInfo         `json:"subagents,omitempty"`
	ArtifactCount           int                    `json:"artifact_count,omitempty"`
	PlanTier                string                 `json:"plan_tier,omitempty"`
	Email                   string                 `json:"email,omitempty"`
	PendingInputCount       int                    `json:"pending_input_count,omitempty"`
	ToolConfirmationPending bool                   `json:"tool_confirmation_pending,omitempty"`
	TaskCount               int                    `json:"task_count,omitempty"`
	TerminalWidth           int                    `json:"terminal_width,omitempty"`
	Cost                    *CostInfo              `json:"cost,omitempty"`
}

type ModelInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Effort      string `json:"effort,omitempty"`
}

type WorkspaceInfo struct {
	CurrentDir string `json:"current_dir"`
	ProjectDir string `json:"project_dir"`
}

type ContextInfo struct {
	TotalInputTokens    int               `json:"total_input_tokens"`
	TotalOutputTokens   int               `json:"total_output_tokens"`
	ContextWindowSize   int               `json:"context_window_size"`
	UsedPercentage      *float64          `json:"used_percentage"`
	RemainingPercentage *float64          `json:"remaining_percentage"`
	CurrentUsage        *CurrentUsageInfo `json:"current_usage"`
}

type CurrentUsageInfo struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type CostInfo struct {
	TotalUSD    float64 `json:"total_usd"`
	SubagentUSD float64 `json:"subagent_usd"`
	Estimated   bool    `json:"estimated"`
}

type QuotaBucket struct {
	RemainingFraction *float64 `json:"remaining_fraction,omitempty"`
	RemainingAmount   *int64   `json:"remaining_amount,omitempty"`
	ResetTime         *string  `json:"reset_time,omitempty"`
	ResetInSeconds    *int64   `json:"reset_in_seconds,omitempty"`
	Disabled          bool     `json:"disabled,omitempty"`
}

type VCSInfo struct {
	Type   string `json:"type"`
	Branch string `json:"branch,omitempty"`
	Client string `json:"client,omitempty"`
	Dirty  bool   `json:"dirty,omitempty"`
}

type SandboxInfo struct {
	Enabled      bool `json:"enabled"`
	AllowNetwork bool `json:"allow_network,omitempty"`
}

type SubagentInfo struct {
	Name   string `json:"name"`
	Role   string `json:"role,omitempty"`
	Model  string `json:"model,omitempty"`
	Status string `json:"status"`
}

// RenderOptions controls explicit overrides for rendering or demo previews.
type RenderOptions struct {
	SingleLine bool
	ThemeName  string
}

// FleetAccountView represents a non-active account managed by agy-swap.
type FleetAccountView struct {
	Email           string
	Alias           string
	Tier            string
	Status          string // "ready", "partial", "cooldown", "unavailable"
	PrimaryPct      float64
	SecondaryPct    float64
	HasSecondary    bool
	LowQuotaBlocked bool
	WaitSeconds     int64
}

// AgySwapInfo holds read-only account alias and reserve fleet state.
type AgySwapInfo struct {
	Alias         string
	ActiveTier    string
	ReadyReserves int
	TotalAccounts int
	FallbackQuota map[string]QuotaBucket
	Fleet         []FleetAccountView
}

// SessionView holds tracked elapsed time and recent context token delta.
type SessionView struct {
	ElapsedSec        int64
	ActiveDeltaTokens int
}

// GitDetail stores probed Git working tree and branch status.
type GitDetail struct {
	Branch     string `json:"branch"`
	IsWorktree bool   `json:"is_worktree"`
	Ahead      int    `json:"ahead"`
	Behind     int    `json:"behind"`
	Staged     int    `json:"staged"`
	Modified   int    `json:"modified"`
	Untracked  int    `json:"untracked"`
	Conflicts  int    `json:"conflicts"`
	Dirty      bool   `json:"dirty"`
	Probed     bool   `json:"probed"`
}

type agySwapConfig struct {
	Aliases map[string]string `json:"aliases"`
	Policy  struct {
		MinRemainingPct int `json:"min_remaining_pct"`
	} `json:"policy"`
}

type agySwapAccount struct {
	Email         string                       `json:"email"`
	Name          string                       `json:"name"`
	SecretRef     string                       `json:"secret_ref"`
	QuotaLimits   map[string]agySwapQuotaLimit `json:"quota_limits"`
	QuotaSnapshot *agySwapQuotaSnapshot        `json:"quota_snapshot"`
}

type agySwapQuotaLimit struct {
	Family  string `json:"family"`
	Model   string `json:"model"`
	ResetAt string `json:"reset_at"`
}

type agySwapQuotaSnapshot struct {
	ObservedAt string              `json:"observed_at"`
	Tier       any                 `json:"tier"`
	Groups     []agySwapQuotaGroup `json:"groups"`
}

type agySwapQuotaGroup struct {
	ID      string               `json:"id"`
	Name    string               `json:"name"`
	Buckets []agySwapQuotaBucket `json:"buckets"`
}

type agySwapQuotaBucket struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	RemainingFraction float64 `json:"remaining_fraction"`
	ResetAt           string  `json:"reset_at"`
	Window            string  `json:"window"`
}

type fleetCacheEntry struct {
	ObservedAt string                 `json:"observed_at"`
	UpdatedAt  int64                  `json:"updated_at"`
	Tier       string                 `json:"tier,omitempty"`
	Quota      map[string]QuotaBucket `json:"quota"`
}

type sessionStateEntry struct {
	StartedAt   int64 `json:"started_at"`
	LastSeen    int64 `json:"last_seen"`
	LastTokens  int   `json:"last_tokens"`
	DeltaTokens int   `json:"delta_tokens"`
	DeltaUntil  int64 `json:"delta_until"`
}
