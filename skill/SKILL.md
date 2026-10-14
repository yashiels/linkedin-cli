---
name: linkedin
description: Use lnk for LinkedIn jobs, profiles and account actions.
---

# linkedin

`lnk` is a command-line interface for LinkedIn built on LinkedIn's internal
Voyager API (reverse-engineered from the Android app). Search and view job
postings, apply via Easy Apply, view member profiles, browse the recommended
job feed, and manage saved jobs and job-alert subscriptions — all from the
shell. Every data command supports `--json` for machine-readable output.

> Unofficial tool, not affiliated with LinkedIn. It talks to LinkedIn's private
> API, so endpoints can break and automated access can violate LinkedIn's rules
> and lead to restrictions or account closure. Request pacing is load control,
> not authorization, a safe threshold, human mimicry or ban immunity.

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
| `lnk auth status` | Show local authentication state; no network |
| `lnk auth logout` | Remove stored credentials |
| `lnk status` | With credentials, quota-counted live Ping then `/me`; may consume two admissions |

**Where credentials live:** `~/.config/lnk/credentials.json`, written atomically
with `0600` permissions (directory `0700`). JSON fields: `li_at`, `csrf_token`,
`bcookie`.

**Environment-variable overrides** (take precedence over the file — useful for
explicitly authorized headless use without writing cookies to disk):

| Variable | Description |
|---|---|
| `LNK_LI_AT` | `li_at` session cookie |
| `LNK_CSRF_TOKEN` | CSRF token (the value after `ajax:` from `JSESSIONID`) |
| `LNK_CONFIG` | Path to config file |
| `NO_COLOR` | Disable ANSI colour |

Nonempty cookie overrides apply per field after file loading; invalid/unreadable
stored JSON still fails. Config precedence is `--config`, then `LNK_CONFIG`, then
`~/.config/lnk/config.toml`; missing keys use defaults. `LNK_DEBUG` is unsupported.
Never print cookies or extract them from a browser/keychain automatically. Login
and credential replacement require owner consent and out-of-band cookie entry.

## Request controls and recovery

Default policy: minimum five seconds between admissions, maximum 100 admissions
per rolling 24 hours, one host-user-wide in-flight LinkedIn request. Reads, writes,
Ping and compatibility fallbacks all consume admissions; commands may use several.
Deduplicate and bound reads; do not set up unlimited polling. Other machines, apps
and older binaries are not counted. Admissions do not measure exact wire packets:
Go transport internal retries or trusted custom transports are a limitation.

Only stricter TOML settings are supported:

```toml
[safety]
delay_seconds = 5
budget = 100
```

Delay must be an integer from 5 through 9223371916 seconds; budget an integer from
1 through 100. Unknown safety keys are invalid (exit 2). Delays beyond 24 hours
still use the retained last admission after rolling history expires.

No application-layer retry follows refusal, transport or body failure. First 429
persists max(30 minutes, server `Retry-After`); 401/403/999 and recognized same-origin
login/authwall/challenge redirects latch manual review. A resource 403 does not
prove session expiry or permanent ban. Unknown body errors fail the command without
guessing an auth latch. Profile compatibility fallbacks are separate paced
admissions; typed request failures stop them, except 410 at the deprecated REST
profile caller.

`lnk safety status --json` is local, reads no credentials, and initializes metadata
on first use. JSON stdout fields are `admissions`, `budget`, `delay_seconds`,
`cooldown` (UTC timestamp, zero `0001-01-01T00:00:00Z`), and `block` (empty,
`auth`, or `retry_after_overflow`). It can return success while blocked; inspect
the fields. Both `lnk --json safety status` and the trailing flag work.

Only after official app/browser review and the owner's explicit instruction, run
`lnk safety resume --confirm --json`. It clears the manual auth stop locally,
without network or credential changes. It preserves cooldown and unexpired history,
cannot clear the overflow guard or repair unsafe/corrupt state. Never automatically
resume, delete policy state, reset budget, refresh credentials or solve challenges.

State is fixed at `~/.local/share/lnk/safety`, independent of config path and cookies.
It records timestamps and enumerated stop metadata, not credentials or profile data.
Locked operations prune admissions at least 24 hours old, retaining last admission,
greatest observed clock and refusal metadata. Clock rollback and unsafe state fail
closed. Owner-only modes (directory 0700, files 0600), no extended ACLs/symlinks,
supported local filesystem and locking/sync semantics are required. Restrictive
umasks that remove required owner bits can fail closed; never weaken validation.

Success is exit 0; general/body/API errors 1; usage/config/missing resume confirmation
2; auth/refusal stop 3; transport/state/clock failure 4; budget/cooldown/overflow/lock
timeout 5. Root `--json` errors go to stderr with `error`, `code`, `reason` and
optional allowlisted `method`, `operation`, numeric `status`. Successful data goes
to stdout. Verbose/debug diagnostics contain bounded method/status only, no full
URLs, credentials or bodies. Explicit data JSON still contains requested data.

Legacy Android-looking 4.1.1209/build 211700 headers are compatibility metadata;
Go transport and browser cookies do not prove native Android identity. The tracking
duplicate is fixed, not the full current APK catalogue integrated. “Other” is an
opaque session label, not a detection verdict. Keep working query IDs: absent newer
APK literals are drift evidence, not removal proof. Never spoof attestation, device
IDs or TLS, rotate proxies, or create logins to change the label.

For filesystem details, Retry-After overflow and diagnostics, read the repository's
[request compatibility reference](../docs/request-compatibility.md).

## Search & jobs

| Command | Description |
|---|---|
| `lnk search <keywords> [flags]` | Search job listings |
| `lnk job <job-id> [--open] [--check-apply]` | View full details; `--check-apply` verifies the current application route and may anonymously fetch a supported employer ATS URL |
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

Job JSON preserves all existing fields and adds `application`:

- `status`: `accepting`, `closed`, or `unverified`.
- `source`, `evidence`, and `reason`: what supports the status and why.
- `checkedAt`: when the current LinkedIn or employer check ran.
- `applyUrl`: a real employer URL observed in LinkedIn data, when present. This
  is distinct from `listingUrl`.
- `applicantTrackingSystem`: the ATS name observed in LinkedIn data, when present.

For one observed URL, empty ATS metadata is ignored when a real provider name is
also present. Conflicting non-empty names clear the provider and remain
`unverified`. Multiple observed URLs retain one deterministic URL without choosing
an ATS, so no arbitrary destination is externally verified.

Use `lnk job <job-id> --check-apply --json` before treating a discovery result as
apply-now. `accepting` requires LinkedIn `LISTED` plus `onsiteApply: true` and the
exact Easy Apply CTA, an identified Lever POST form with an active submission
control inside it, or Workday JSON with boolean `jobPostingInfo.canApply: true`.
`closed` requires LinkedIn `CLOSED`/`SUSPENDED` or Workday JSON
`jobPostingInfo.canApply: false`. Everything else is `unverified`: missing or disabled controls,
unknown job state, unsupported ATS sites, login/challenge walls, failed requests,
and pages that merely return HTTP 200. `expired: false`, `jobState: LISTED`, a
generated listing URL, a form of an unknown kind, or “Apply” text in a description
are not proof.

External verification is intentionally narrow. Lever and Workday are the only
supported employer checks. Workday landing HTML is not JSON proof and remains
`unverified`. The checker uses HTTPS only, permits at most three same-posting safe
redirects, has an eight-second timeout and 1 MiB body limit, rejects non-public
destinations, closes idle connections after each verification, and never sends
LinkedIn cookies, Authorization, or CSRF headers to employers.

Lever uses the official Go HTML5 parser. A submission control must belong to the
validated form, preserve POST and the posting target, and have no supported hidden
signal on itself or an ancestor: `hidden`, `aria-hidden=true`, a `hidden` class,
or inline `display:none`/`visibility:hidden`. Full stylesheet layout is outside
the verifier's scope.

Lever free text is never treated as closure proof. If its parsed application form
and control checks fail, the result is `unverified`; dated deadlines remain a
separate manual check against the current job description.

| Command | Description |
|---|---|
| `lnk apply <job-id> [--dry-run] [--confirm]` | Apply via Easy Apply |

`apply` fetches the job, checks Easy Apply availability, shows what will be
submitted (your profile data), asks for confirmation, then submits. A known-closed
job is rejected. If Easy Apply is unavailable, an observed employer URL is labeled
unverified, or the command states that no employer URL was observed. It never
labels the generated LinkedIn listing URL as an employer URL. `--dry-run` previews
without submitting, but still makes quota-counted live reads; it is not the default.
`--confirm` skips the prompt. `--no-input` or missing TTY without `--confirm`
fails instead of prompting. Never supply `--confirm` automatically.

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
pipe-friendly), `--no-color`, `--quiet`/`-q`, `--verbose` and `--debug` (bounded HTTP method/status diagnostics on
stderr, without URLs or bodies), `--no-input` (fail instead of
prompting), `--config <path>`.

## Headless / agent usage

**Owner-authorized bounded reads can run without prompts once credentials exist:** `search`,
`job` (without `--open`, including `--check-apply`), `feed`, `profile`, `saved list`, `alerts list`,
`status`, `auth status`, `safety status`. This does not grant account access or
permission for unlimited polling and does not remove account risk. Add `--json` (or `--plain`) to parse output. Avoid
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

**Write / action commands require exact owner consent; not all have CLI prompts:**

- `lnk apply <job-id>` — submits a real job application. It prompts for
  confirmation by default; **preview first with `--dry-run`.** `--confirm`
  bypasses the prompt — never pass it automatically.
- `lnk saved add` / `lnk saved remove` — modify the user's saved-jobs list.
- `lnk alerts create` / `lnk alerts delete` — modify job-alert subscriptions.
- `lnk auth logout` — deletes stored credentials.
- `lnk safety resume --confirm` — clears a manual policy stop after owner review.

Never run an application, saved-job, or alert write without explicit consent for
that exact action. Discovery is not consent.

## Agent job-verification workflow

1. Deduplicate daily search/feed results by job `id` before fetching details.
2. Run `lnk job <job-id> --check-apply --json` for each candidate under review.
3. Check the current description for a closing date. A passed or ambiguous date
   excludes the job from apply-now even if the route reports `accepting`.
4. Include only `application.status == "accepting"` in an apply-now shortlist.
   Put `unverified` entries in a clearly separate manual-review queue.
5. Evaluate suitability separately. State missing qualifications or experience
   explicitly; CLI application proof does not establish candidate fit.

## Typical flow

```sh
# 1. Authenticate once (paste li_at + JSESSIONID from a browser session)
lnk auth login
lnk status                 # confirm logged in + API connected

# 2. Search and inspect jobs
lnk search "backend engineer" --location "Cape Town" --easy-apply --json
lnk job 4414623196 --check-apply --json
lnk feed --limit 10        # personalised recommendations

# 3. Preview then apply (Easy Apply)
lnk apply 4414623196 --dry-run   # preview submission
lnk apply 4414623196             # apply (asks for confirmation)

# 4. Track and stay notified
lnk saved add 4414623196
lnk alerts create --keywords "backend engineer" --location "Remote" --frequency weekly
```
