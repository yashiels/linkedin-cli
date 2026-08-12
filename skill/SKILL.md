---
name: linkedin
description: LinkedIn from the terminal via the lnk CLI. Search and view jobs, apply via Easy Apply, view profiles, manage saved jobs and job alerts.
---

# linkedin

`lnk` is a command-line interface for LinkedIn built on LinkedIn's internal
Voyager API (reverse-engineered from the Android app). Search and view job
postings, apply via Easy Apply, view member profiles, browse the recommended
job feed, and manage saved jobs and job-alert subscriptions — all from the
shell. Every data command supports `--json` for machine-readable output.

> Unofficial tool, not affiliated with LinkedIn. It talks to LinkedIn's private
> API, so it may break if endpoints change, can be rate-limited (backs off on
> HTTP 429), and automated access may violate LinkedIn's Terms of Service.

## Install

```sh
brew install yashiels/tap/lnk
```

Or `go install github.com/yashiels/linkedin-cli/cmd/lnk@latest`, or grab a
pre-built binary (macOS/Linux, arm64/amd64) from the Releases page. The binary
is named `lnk`.

## Auth

Authentication is **session-cookie based — not OAuth**. `lnk` reuses the cookies
from an existing logged-in LinkedIn browser session. You need two values (plus
one optional one), which you copy manually from your browser's cookie storage
(DevTools → Application → Cookies → `https://www.linkedin.com`):

- **`li_at`** (required) — the primary session cookie (long hex/JWT string).
- **`JSESSIONID`** (required) — of the form `ajax:<token>`; `lnk` strips the
  `ajax:` prefix and stores the remaining value as the CSRF token.
- **`bcookie`** (optional) — the browser-identifier cookie.

A session is considered logged in only when **both `li_at` and the CSRF token**
are present. There is no automated cookie extraction and no browser-driver — you
must obtain the cookies out of band from a real logged-in session.

| Command | Description |
|---|---|
| `lnk auth login` | Interactively prompt for `li_at`, `JSESSIONID` (`ajax:<token>`), and optional `bcookie`; save them |
| `lnk auth status` | Show current authentication state |
| `lnk auth logout` | Remove stored credentials |
| `lnk status` | Show auth state, config path, and live API connectivity |

**Where credentials live:** `~/.config/lnk/credentials.json`, written atomically
with `0600` permissions (directory `0700`). JSON fields: `li_at`, `csrf_token`,
`bcookie`.

**Environment-variable overrides** (take precedence over the file — useful for
CI or headless use without writing cookies to disk):

| Variable | Description |
|---|---|
| `LNK_LI_AT` | `li_at` session cookie |
| `LNK_CSRF_TOKEN` | CSRF token (the value after `ajax:` from `JSESSIONID`) |
| `LNK_CONFIG` | Path to config file |
| `LNK_DEBUG` | `1` to enable debug HTTP logging |
| `NO_COLOR` | Disable ANSI colour |

## Search & jobs

| Command | Description |
|---|---|
| `lnk search <keywords> [flags]` | Search job listings |
| `lnk job <job-id> [--open]` | View full details of a posting (`job-id` bare numeric or `urn:li:fsd_jobPosting:<id>`); `--open` opens in browser |
| `lnk feed [-n/--limit N]` | Recommended job feed (Jobs You May Be Interested In) |

`search` flags:

| Flag | Short | Default | Description |
|---|---|---|---|
| `--location` | `-l` | | Location name or geo URN |
| `--type` | `-t` | | Comma-separated: `full-time`, `part-time`, `contract`, `temporary`, `internship`, `volunteer` |
| `--experience` | `-e` | | Comma-separated: `internship`, `entry`, `associate`, `mid-senior`, `director`, `executive` |
| `--easy-apply` | | false | Easy Apply jobs only |
| `--remote` | | false | Remote jobs only |
| `--sort` | | `relevant` | `recent` or `relevant` |
| `--posted` | | | Posted within `24h`, `week`, `month` |
| `--limit` | `-n` | `25` | Max results |

## Apply

| Command | Description |
|---|---|
| `lnk apply <job-id> [--dry-run] [--confirm]` | Apply via Easy Apply |

`apply` fetches the job, checks Easy Apply availability, shows what will be
submitted (your profile data), asks for confirmation, then submits. If Easy
Apply is not available it prints the external application URL and submits
nothing. `--dry-run` previews without submitting; `--confirm` skips the prompt.

## Saved jobs

| Command | Description |
|---|---|
| `lnk saved list [--limit N]` | List saved jobs |
| `lnk saved add <job-id>` | Save a job |
| `lnk saved remove <job-id>` | Remove a saved job |

## Job alerts

| Command | Description |
|---|---|
| `lnk alerts list` | List alert subscriptions |
| `lnk alerts create --keywords <kw> [--location <loc>] [--frequency daily\|weekly]` | Create an alert (`--keywords` required, frequency defaults `daily`) |
| `lnk alerts delete <alert-id>` | Delete an alert |

## Profile

| Command | Description |
|---|---|
| `lnk profile` | View your own profile |
| `lnk profile <username>` | View another member (username = the slug from `linkedin.com/in/<username>`) |

## Global flags

Work on every command: `--json` (machine-readable), `--plain` (tab-separated,
pipe-friendly), `--no-color`, `--quiet`/`-q`, `--verbose` (log HTTP requests to
stderr), `--debug` (full request/response bodies), `--no-input` (fail instead of
prompting), `--config <path>`.

## Headless / agent usage

**Read commands are safe to run unattended once credentials exist:** `search`,
`job` (without `--open`), `feed`, `profile`, `saved list`, `alerts list`,
`status`, `auth status`. Add `--json` (or `--plain`) to parse output. Avoid
`job --open`, which launches a browser.

**Auth headless:** `lnk` needs the `li_at` + CSRF cookies from an existing
logged-in LinkedIn session — it **cannot bootstrap login without a browser**
(there is no OTP/password flow and no cookie scraper). To run fully headless,
provide the cookies via environment variables so nothing is written to disk:

```sh
export LNK_LI_AT="<li_at cookie>"
export LNK_CSRF_TOKEN="<token after ajax: from JSESSIONID>"
lnk status --json          # verify: should report logged in + API connected
```

Otherwise run `lnk auth login` once (interactive prompt) on a machine where a
human can paste the cookies. An agent **cannot** complete `auth login` alone —
the cookie values come from the user's browser, out of band. Pass `--no-input`
to any command to make it fail loudly instead of blocking on a prompt when
credentials are missing.

**Write / action commands — treat as confirm-gated, run only when the user
explicitly asks:**

- `lnk apply <job-id>` — submits a real job application. It prompts for
  confirmation by default; **preview first with `--dry-run`.** `--confirm`
  bypasses the prompt — never pass it automatically.
- `lnk saved add` / `lnk saved remove` — modify the user's saved-jobs list.
- `lnk alerts create` / `lnk alerts delete` — modify job-alert subscriptions.
- `lnk auth logout` — deletes stored credentials.

## Typical flow

```sh
# 1. Authenticate once (paste li_at + JSESSIONID from a browser session)
lnk auth login
lnk status                 # confirm logged in + API connected

# 2. Search and inspect jobs
lnk search "backend engineer" --location "Cape Town" --easy-apply --json
lnk job 4414623196         # full detail for one posting
lnk feed --limit 10        # personalised recommendations

# 3. Preview then apply (Easy Apply)
lnk apply 4414623196 --dry-run   # preview submission
lnk apply 4414623196             # apply (asks for confirmation)

# 4. Track and stay notified
lnk saved add 4414623196
lnk alerts create --keywords "backend engineer" --location "Remote" --frequency weekly
```
