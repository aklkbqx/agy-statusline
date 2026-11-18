# Configuration

`agy-statusline` works with zero configuration out of the box and integrates with both Google Antigravity CLI (`agy`) and [`agy-swap`](https://github.com/aklkbqx/agy-swap).

## `settings.json`

`agy-statusline install` edits only the `"statusLine"` object in `~/.gemini/antigravity-cli/settings.json` (while preserving all other keys):

```json
{
  "statusLine": {
    "type": "command",
    "command": "/path/to/agy-statusline",
    "enabled": true,
    "stack_with_default": true
  }
}
```

- Set `"stack_with_default": true` (default) to render `agy`'s native mode/model/retry row above the 2-row HUD.
- Set `"stack_with_default": false` (or run `agy-statusline install --replace`) to replace `agy`'s built-in row entirely.

## Environment variables

| Variable | Values | Default | Effect |
| :--- | :--- | :--- | :--- |
| `AGY_STATUSLINE_THEME` | `tokyo-night`, `catppuccin`, `gruvbox`, `nord`, `dracula`, `mono` | Auto-matched from `settings.json` `"colorScheme"` (fallback: `tokyo-night`) | Selects the 24-bit TrueColor palette and surface pill tinting |
| `AGY_STATUSLINE_SINGLE_LINE` | `1`, `true` | `0` | Forces single-row mode at all terminal widths (with inline `⇄N` swap badge on low quota) |
| `AGY_STATUSLINE_NO_BLINK` | `1`, `true` | `0` | Disables terminal SGR `5` hardware blink on critical alerts (`<= 15%` quota, `CONFIRM`, top swap target) |
| `AGY_CLI_HIDE_ACCOUNT_INFO` | `1`, `true` | `0` | Hides active account alias/tier on Row 1 and hides the entire Row 2 multi-account Fleet Deck |
| `AGY_SWAP_DIR` | Directory path | `~/.gemini/agy-swap` | Overrides where read-only `config.json` and `accounts.json` are read from |
| `AGY_STATUSLINE_STATE_DIR` | Directory path | `$XDG_CACHE_HOME/agy-statusline` or `~/Library/Caches/agy-statusline` | Overrides where ephemeral fleet quota cache, session duration/delta, and 4s Git porcelain cache are written |
| `AGY_SETTINGS_PATH` | File path | `~/.gemini/antigravity-cli/settings.json` | Overrides which Antigravity CLI `settings.json` is inspected for `colorScheme` and edited by `install`/`uninstall` |
| `NO_COLOR` | Any non-empty value | unset | Disables all ANSI color and SGR blink output |

## Account aliases & privacy

- When `~/.gemini/agy-swap/config.json` defines `"aliases"`, short alias names take priority over the Google profile name in `accounts.json`.
- If an active email is not found in `agy-swap`, only the local part before `@` (clipped to 10 characters) is displayed — full email addresses are never printed.
- `agy-statusline` never reads `vault.json`, `oauth_creds.json`, or the OS Keychain.
