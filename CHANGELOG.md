# Changelog

## [0.1.1] - 2026-11-19

### Fixed
- **Fail-open statusline guard (`exit status 127` prevention)**: `agy-statusline install` now wraps the `statusLine.command` entry in a POSIX/Windows shell check that falls back to `PATH` and exits `0` cleanly when the binary is absent, preventing `agy`'s 30-failure auto-disable banner (`--raw-command` restores a bare path).
- **Compatibility symlink**: When installing into `~/.gemini/antigravity-cli/settings.json`, `agy-statusline` maintains an atomic compatibility symlink at `~/.gemini/antigravity-cli/bin/agy-statusline` (removed cleanly on `uninstall`) without disturbing sibling files such as `agentapi`, so long-lived open `agy` sessions recover without restarting.
- **Atomic binary replacement**: `Makefile install` and `install.sh` now stage into a temporary file inside the destination directory and rename atomically (`mv -f`) so concurrent 300ms statusline polls never race a deleted binary.

## v0.1.0 (2026-11-18)

### Features
- Initial public release of `agy-statusline` with zero external Go dependencies.
- Full-width Left ↔ Right justified 2-row Cockpit HUD across 7 responsive terminal breakpoints (`>=144` down to `<48` cols) with Thai/Unicode combining-mark width precision.
- Seamless half-step Box-Drawing laser gauges (`━╸─`), surface pills, and 6 built-in themes (`tokyo-night`, `catppuccin`, `gruvbox`, `nord`, `dracula`, `mono`) auto-matched to `settings.json` `colorScheme` or `AGY_STATUSLINE_THEME`.
- Multi-account `agy-swap` Fleet Deck (Row 2) with pool-aware sorting (`ready`, `partial`, `cooldown`, `unavailable`), live cross-account quota memory, and `AGY_STATUSLINE_SINGLE_LINE=1` / `AGY_CLI_HIDE_ACCOUNT_INFO=1` toggles.
- 3-layer hybrid motion: active-turn Braille spinner & RGB shimmer, zero-CPU SGR `5` hardware pulse on critical limits, and live `+Nk` context delta & elapsed duration tracking.
- Cached Git porcelain v2 probe (`GIT_OPTIONAL_LOCKS=0`, 4s TTL) with linked worktree detection (`⧉`).
- Built-in CLI subcommands: `install`, `uninstall`, `doctor`, `demo`, `themes`, `render`, and `version`.
