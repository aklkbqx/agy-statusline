# Contributing

`agy-statusline` is a small, zero-dependency Go binary designed for minimal startup latency (`~3ms`) and clean terminal cell rendering inside Google Antigravity CLI (`agy`). Contributors are expected to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Local verification

Before opening a pull request, run the full local verification suite:

```sh
make verify
```

This checks `gofmt`, repository hygiene, `go vet ./...`, `go test -race ./...`, and benchmarks.

## Guidelines

- Keep zero third-party Go dependencies (`go.mod` uses the Go standard library only).
- Use strictly 1-cell Unicode glyphs (`DisplayWidth` must match rendered terminal cells; avoid wide emojis or `FE0F` selectors).
- Never add code that reads `vault.json`, `oauth_creds.json`, or OS Keychain credentials.
- No GitHub Actions or cloud CI workflows are used; verify changes locally with `make verify`.

## Building a release archive

Cross-compile deterministic archives and `SHA256SUMS` into `dist/release/`:

```sh
make release-assets VERSION=0.1.0
```
