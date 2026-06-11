# AGENTS.md — lnk

LinkedIn CLI built on the reverse-engineered Voyager API (Android APK). Go 1.26+, Cobra, RestLi encoding.

## Structure

```
.
├── cmd/
│   └── lnk/
│       └── main.go          # Entry point — wires Cobra root command and version injection
├── internal/
│   ├── api/                 # LinkedIn Voyager API client
│   │   ├── client.go        # HTTP client, cookie jar, base request helpers
│   │   ├── rest.go          # RestLi protocol helpers
│   │   ├── jobs.go          # Job search endpoint
│   │   ├── jobdetail.go     # Single job detail fetch
│   │   ├── apply.go         # Easy Apply submission
│   │   ├── filters.go       # Search filter encoding
│   │   ├── post.go          # Feed/post fetching
│   │   ├── profile.go       # Profile endpoint
│   │   └── saved.go         # Saved jobs management
│   ├── auth/
│   │   └── store.go         # Cookie persistence (li_at + JSESSIONID) via TOML
│   ├── cmd/                 # Cobra sub-command implementations
│   │   ├── auth.go          # lnk auth login/logout/status
│   │   ├── search.go        # lnk search
│   │   ├── job.go           # lnk job <id>
│   │   ├── apply.go         # lnk apply <id>
│   │   ├── profile.go       # lnk profile
│   │   ├── saved.go         # lnk saved
│   │   ├── alerts.go        # lnk alerts
│   │   ├── feed.go          # lnk feed
│   │   ├── status.go        # lnk status
│   │   └── helpers.go       # Shared output helpers (table + JSON)
│   ├── config/
│   │   └── config.go        # Config file path resolution
│   ├── html/
│   │   └── html.go          # HTML tag stripper for job descriptions
│   ├── output/
│   │   └── output.go        # Table and --json renderer
│   ├── restli/
│   │   └── encoder.go       # RestLi variable encoding (LinkedIn-specific protocol)
│   └── types/               # Shared domain types
│       ├── jobs.go
│       ├── profile.go
│       ├── alert.go
│       ├── urn.go
│       └── errors.go
├── docs/
│   └── assets/              # Banner and hero images (README only)
├── .github/
│   └── workflows/
│       ├── ci.yml           # Lint + test on push / PR
│       └── release.yml      # Cross-platform build + GitHub Release + Homebrew tap
├── api-reference.json       # Reference snapshot of Voyager API (NOT a runtime dep)
├── prd.json                 # Product requirements snapshot (NOT a runtime dep)
├── API-REFERENCE.md         # Human-readable API reference
├── go.mod / go.sum
├── Makefile
└── .golangci.yml
```

## Build / Test / Lint

```bash
# Run the full CI gate (vet + fmt check + race tests — mirrors ci.yml)
make ci

# Run linter (golangci-lint if installed, falls back to go vet)
make lint

# Run tests with race detector
make test

# Build the lnk binary into the repo root
make build

# Format all Go source files with gofmt
make fmt

# Remove compiled binary
make clean
```

> **Run locally before every commit:** `make ci` must pass cleanly — CI enforces the same checks.

## Key Design Decisions

- **RestLi encoding** — LinkedIn's Voyager API uses a custom RestLi variable syntax. `internal/restli/encoder.go` handles this encoding. Do not replace it with plain query-string encoding; the API will reject requests.
- **Cookie auth** — Authentication uses `li_at` and `JSESSIONID` session cookies, not OAuth. Cookies are stored in a TOML file via `internal/auth/store.go`. Do not introduce OAuth flows.
- **HTML stripping** — Job descriptions from the API contain raw HTML. `internal/html/html.go` strips tags before display. Keep this layer; removing it exposes raw markup to the terminal.
- **Cobra + table/JSON output** — All sub-commands use Cobra. Every command that returns data must support `--json` for machine-readable output. The `internal/output` package handles both table and JSON rendering consistently.

## Constraints

- **Do not switch from Cobra** — the CLI surface, completion, and help text all depend on it.
- **Do not add OAuth** — the tool is intentionally cookie-based; OAuth would require an official LinkedIn app registration.
- **Do not remove or rewrite the RestLi encoder** — `internal/restli/encoder.go` is a correctness-critical component. Changes must be validated against the existing encoder tests.
- **`prd.json` and `api-reference.json` are reference docs, not runtime deps** — they are checked in for developer reference. Do not import or parse them at runtime.
- **Keep `--json` on every data command** — machine-readability is a first-class feature. New commands that return data must include a `--json` flag.

## CI

| Workflow | Trigger | Steps |
|----------|---------|-------|
| `ci.yml` | push to `main`, every PR | `go vet`, gofmt check, `go test -race ./...` |
| `release.yml` | push of `v*` tag | cross-platform build (darwin/linux × arm64/amd64) → GitHub Release → Homebrew tap update |

Run the CI gate locally with:

```bash
make ci
```
