# Security Policy

`agy-statusline` runs strictly locally as a short-lived status line renderer invoked by Google Antigravity CLI (`agy`).

## Safe operation

- **Zero network access & zero telemetry**: `agy-statusline` opens no sockets and makes no HTTP requests.
- **No credential access**: Reads only non-secret account metadata from `~/.gemini/agy-swap/config.json` and `~/.gemini/agy-swap/accounts.json` (plus `~/.gemini/antigravity-cli/settings.json` for `colorScheme` and `install`/`uninstall`). Never reads `vault.json`, `oauth_creds.json`, or OS Keychain entries.
- **Atomic & private state files**: Ephemeral fleet quota memory, session deltas, and Git porcelain caches are stored under a `0700` user-owned cache directory with `0600` atomic file writes.
- **Safe `settings.json` edits**: `install` and `uninstall` validate existing JSON before writing and refuse to overwrite malformed files.
- **Safe Git invocations**: Runs `git` with `--no-optional-locks`, `GIT_OPTIONAL_LOCKS=0`, and a `1.2s` timeout so status rendering never blocks index locks.

## Reporting a vulnerability

Do not open a public issue for an undisclosed vulnerability. Report privately through GitHub private vulnerability reporting on the repository with reproduction steps, OS/terminal details, and affected file paths without personal emails, tokens, or screenshots containing private keys.
