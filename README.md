# lnk — LinkedIn CLI

A fast, unofficial command-line interface for LinkedIn, built on the internal
Voyager API reverse-engineered from the Android APK.

> **Disclaimer:** This is an independent, unofficial tool and is not affiliated
> with, endorsed by, or supported by LinkedIn Corporation. It may break at any
> time if LinkedIn changes their internal API. Use responsibly and at your own
> risk.

---

## Table of Contents

- [Install](#install)
- [Quick Start](#quick-start)
- [Commands](#commands)
  - [auth](#auth)
  - [profile](#profile)
  - [search](#search)
  - [alerts](#alerts)
  - [status](#status)
  - [completion](#completion)
- [Configuration](#configuration)
- [Environment Variables](#environment-variables)
- [How It Works](#how-it-works)
- [License](#license)

---

## Install

### From source (requires Go 1.21+)

```bash
go install github.com/yashiels/linkedin-cli/cmd/lnk@latest
```

### Build locally

```bash
git clone https://github.com/yashiels/linkedin-cli.git
cd linkedin-cli
make install
```

### Pre-built binaries

Download from the [Releases](https://github.com/yashiels/linkedin-cli/releases) page.

---

## Quick Start

1. **Get your session cookies** from the browser:
   - Open [LinkedIn](https://www.linkedin.com) and log in.
   - Open DevTools → Application → Cookies → `linkedin.com`.
   - Copy the values for `li_at` and `JSESSIONID`.

2. **Log in:**
   ```bash
   lnk auth login
   ```
   Paste each cookie value when prompted.

3. **Verify your setup:**
   ```bash
   lnk status
   ```

4. **Search for jobs:**
   ```bash
   lnk search "software engineer" --location "Cape Town"
   ```

---

## Commands

### auth

Manage LinkedIn session credentials.

```bash
# Store your li_at and JSESSIONID cookies
lnk auth login

# Check current auth status
lnk auth status

# Remove stored credentials
lnk auth logout
```

Credentials are stored in `~/.config/lnk/credentials.json` with `0600`
permissions. You can also supply them via environment variables (see
[Environment Variables](#environment-variables)).

---

### profile

View a LinkedIn member profile.

```bash
# View your own profile
lnk profile

# View another member's profile (use their LinkedIn URL slug)
lnk profile satyanadella
lnk profile yashielsookdeo

# Output as JSON
lnk profile --json
lnk profile satyanadella --json
```

**Example output:**

```
Yashiel Sookdeo
Software Engineer at Skyner
Cape Town Metropolitan Area

About:
  Building software systems and developer tools.

Experience:
  • Software Engineer at Skyner (2023 - Present)
  • Junior Developer at Acme Corp (2021 - 2023)

Education:
  • BSc Computer Science, UCT (2018 - 2021)

500+ connections
```

The username is the slug from the LinkedIn profile URL:
`https://www.linkedin.com/in/<username>`

---

### search

Search for job postings.

```bash
lnk search "software engineer" --location "Cape Town"
lnk search "product manager" --location "Remote" --limit 10
lnk search "backend engineer" --location "London" --json
```

---

### alerts

Manage LinkedIn job alert subscriptions.

```bash
# List all active alerts
lnk alerts list

# Create a new alert
lnk alerts create --keywords "software engineer" --location "Cape Town"
lnk alerts create --keywords "product manager" --location "Remote" --frequency weekly

# Delete an alert by ID (find IDs with 'lnk alerts list')
lnk alerts delete 123456789
```

**Example `lnk alerts list` output:**

```
ALERT ID      KEYWORDS            LOCATION       FREQUENCY  CREATED
──────────    ──────────────────  ─────────────  ─────────  ──────────
111222333     software engineer   Cape Town      DAILY      2024-01-15
444555666     product manager     Remote         WEEKLY     2024-03-01
```

**Flags for `lnk alerts create`:**

| Flag          | Default  | Description                              |
|---------------|----------|------------------------------------------|
| `--keywords`  | required | Job search keywords                      |
| `--location`  | (none)   | Location filter (city, country, Remote)  |
| `--frequency` | `daily`  | Email frequency: `daily` or `weekly`     |

---

### status

Show authentication state, configuration, and API connectivity.

```bash
lnk status
lnk status --json
```

**Example output:**

```
✓ Logged in as Yashiel Sookdeo
  Session: active
  Config:  ~/.config/lnk/config.toml
  API:     connected (prod-ltx1)
```

---

### completion

Generate a shell completion script.

```bash
# Bash — source immediately
source <(lnk completion bash)

# Bash — persist across sessions
lnk completion bash > /etc/bash_completion.d/lnk
# or (user-local):
lnk completion bash > ~/.local/share/bash-completion/completions/lnk

# Zsh
lnk completion zsh > "${fpath[1]}/_lnk"
# Reload shell: exec zsh

# Fish
lnk completion fish > ~/.config/fish/completions/lnk.fish

# PowerShell
lnk completion powershell | Out-String | Invoke-Expression
```

After installation, `Tab` completion will work for all `lnk` subcommands
and flags.

---

## Global Flags

These flags work with every command:

| Flag          | Short | Description                                        |
|---------------|-------|----------------------------------------------------|
| `--json`      |       | Output as JSON (machine-readable)                  |
| `--plain`     |       | Output as tab-separated text (pipe-friendly)       |
| `--no-color`  |       | Disable ANSI colour codes                          |
| `--quiet`     | `-q`  | Suppress informational messages                    |
| `--verbose`   |       | Log HTTP requests to stderr                        |
| `--debug`     |       | Log full HTTP request/response bodies to stderr    |
| `--no-input`  |       | Fail instead of prompting for input                |
| `--config`    |       | Path to config file (overrides default location)   |

---

## Configuration

`lnk` reads configuration from `~/.config/lnk/config.toml`:

```toml
[defaults]
location = "Cape Town, South Africa"
sort     = "relevant"   # or "recent"
limit    = 25

[display]
color = true
```

Override the config file path with the `LNK_CONFIG` environment variable or
the `--config` flag.

---

## Environment Variables

| Variable         | Description                                              |
|------------------|----------------------------------------------------------|
| `LNK_LI_AT`      | LinkedIn `li_at` session cookie (overrides stored cred)  |
| `LNK_CSRF_TOKEN` | LinkedIn CSRF token from `JSESSIONID` cookie             |
| `LNK_CONFIG`     | Path to config file                                      |
| `NO_COLOR`       | Disable ANSI colour (standard: https://no-color.org)     |

Setting `LNK_LI_AT` and `LNK_CSRF_TOKEN` lets you use `lnk` in CI/CD
pipelines without writing a credentials file to disk.

---

## How It Works

LinkedIn does not provide an official public API for the features `lnk` targets.
This tool is built by reverse-engineering the LinkedIn Android application
(APK v4.1.1209) using standard decompilation tools.

All requests hit `https://www.linkedin.com/voyager/api/graphql` (and a handful
of REST sub-paths under `/voyager/api/`) with the same HTTP headers that the
official Android client sends. The query IDs and variable encoding format
(RestLi) were extracted directly from the APK's request-building code.

Because this relies on LinkedIn's *internal* API:

- **It may break at any time** if LinkedIn updates or deprecates endpoints.
- **Rate-limiting is possible** — the client applies exponential back-off on
  HTTP 429 responses.
- **LinkedIn's Terms of Service** may prohibit automated access. Review them
  before use and use the tool responsibly.

---

## License

MIT — see [LICENSE](LICENSE).

Copyright (c) 2025 Yashiel Sookdeo
