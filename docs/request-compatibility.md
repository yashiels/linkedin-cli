# Request controls and session compatibility

`lnk` uses LinkedIn's private, unofficial Voyager API. Automated access can violate
LinkedIn's rules and result in restrictions or account closure. Slower requests do
not make automation authorized. No published safe threshold, ban immunity,
human-mimic guarantee or native-session guarantee is established by these controls.
Use requires the owner's acceptance of that risk and authorization for the work.

## Admissions and configuration

All current LinkedIn senders, including Ping and mutations, share one host-user-wide
policy. Defaults are one in-flight request, minimum five seconds between admissions,
and at most 100 admissions in the rolling interval `(now - 24h, now]`. An admission
exactly 24 hours old expires; spacing exactly at the configured delay is allowed.
Concurrent commands may interleave, but the lease covers the complete HTTP exchange,
bounded response buffering and body close. Lock acquisition waits at most 30 seconds
(exit 5). Responses larger than 16 MiB fail with exit 4 and consume their admission.
Admissions are persisted before sending, so crashes and unknown outcomes count.

Budget units are policy admissions, not commands or exact wire packets. A command
can make several reads; `status` with credentials sends Ping then `/me` on success.
`auth status` is local credential inspection. `safety status` is local policy
inspection and does not read credentials. Compatibility fallbacks consume separate,
paced admissions. Profile typed request failures stop fallback, with a 410 exception
only at the deprecated REST profile caller, not at dash, GraphQL or later reads.

No application-layer retry follows refusal, transport or body errors. Go
`net/http` can internally retry certain eligible requests on reused connections;
a trusted custom RoundTripper can implement its own behavior. Policy admission
cannot count every resulting wire attempt. Injected clients are copied and receive the required redirect policy; their
transports still pass through destination validation and admission. Only HTTPS
`www.linkedin.com` Voyager API destinations are accepted. Mutations have redirect following and
body replay disabled; no mutation retry/idempotency policy is added. All LinkedIn
redirects are rejected before following, including same-origin redirects. The
separate employer verifier retains its own bounded anonymous redirect rules.

```toml
[safety]
delay_seconds = 5
budget = 100
```

These are defaults, not LinkedIn-approved thresholds. Only stricter limits are
supported: integer delay from 5 through 9223371916 seconds, integer budget from 1
through 100. Unknown safety keys and invalid values fail with exit 2. The delay
ceiling reserves the request timeout allowance within Go's duration range.
The last admission is retained separately, so a delay longer than 24 hours remains
enforced after its timestamp leaves the rolling budget list.

Config selection is `--config` > nonempty `LNK_CONFIG` >
`~/.config/lnk/config.toml`; omitted keys use built-ins. Credentials remain JSON at
`~/.config/lnk/credentials.json`, with `li_at`, `csrf_token`, optional `bcookie`.
Nonempty `LNK_LI_AT` and `LNK_CSRF_TOKEN` override individual loaded fields; invalid
or unreadable stored JSON still fails. Both session values are needed. `NO_COLOR`
disables color. There is no production `LNK_DEBUG` environment setting.

## Refusal and recovery

| Signal | Effect |
|---|---|
| First HTTP 429 | No retry; persist cooldown through max(now + 30 minutes, server Retry-After), exit 5 |
| HTTP 401, 403, 999 | Persist `block: "auth"`, exit 3 |
| Same-origin recognized auth redirect | Persist `auth`, exit 3; never follow |
| Other redirect | Reject current command, exit 4; no guessed auth latch |
| Transport/read/oversize failure | Fail current command, exit 4; admission remains counted |
| Invalid JSON/HTML where JSON expected or nonempty top-level GraphQL errors | Fail current command, generally exit 1; no body-inferred auth latch |

Recognized redirect paths are normalized `/login`, `/uas/login`, `/authwall`, and
`/checkpoint/challenge`, including descendants and encoded variants. A 403 can be
resource-local; conservatively stopping does not identify session expiry, a temporary
restriction or a permanent ban. Unknown response text is not a verified restriction
contract. Other unsuccessful HTTP statuses normally produce general API failure.

`Retry-After` accepts seconds or HTTP dates. Missing, invalid or past values use
the 30-minute floor. Longer representable deadlines are not capped downward.
Unrepresentable deadlines persist `retry_after_overflow`, which blocks admissions
with exit 5 and cannot be cleared by resume.

1. Stop live commands and inspect `lnk safety status --json` locally.
2. Have the owner inspect the official app/browser and decide whether to continue.
3. Only on explicit owner instruction after review, clear the manual auth stop:

```sh
lnk safety resume --confirm --json
```

This command has no network or credential effect. It clears `auth`, preserves
cooldown and unexpired admission history, and returns the same status schema.
It can succeed during cooldown: that does not authorize a new admission yet.
It cannot repair corrupt/unsafe state, fix clock rollback, clear overflow or reset
budget. Do not delete policy state or its lock/initialization files as routine
recovery. No automatic login, credential refresh, CAPTCHA solving or session
revocation is part of recovery.

## Local state and limits

The fixed namespace is `~/.local/share/lnk/safety`, containing `lock`, `initialized`
and `state.json`. It is independent of `--config`, `LNK_CONFIG`, cookie values and
account identity; different accounts on one host user intentionally share it.
Other hosts, browsers, apps and older binaries are not counted. It has no distributed
coordination or automatic background polling. Deliberate deletion of the entire
namespace by the same user is outside the accidental-load threat model.

Status is local but not read-only: it acquires the lease, initializes metadata on
first use, advances the greatest observed UTC wall clock, prunes expired admissions
and saves state. Resume does the same before clearing an eligible stop. Neither
consumes an admission. Successful status can report a block or active cooldown.

State contains version, admission timestamps, last admission, greatest observed
clock, cooldown and enumerated block; no cookie values, account identifiers or
profile payloads. Successful locked observations prune admissions at least 24 hours
old. Last admission/clock and refusal metadata remain; an elapsed cooldown timestamp
can remain visible even though it no longer blocks.

Only absence of both marker and state permits initialization. Missing one, corrupt
or unreadable state, unexpected ownership, symlinks, nonregular/hard-linked files,
unsafe ancestor permissions, or invalid modes fail closed. The private directory
requires exactly 0700, files 0600 and no extended ACL entries. A restrictive umask
removing required owner bits can prevent initialization; existing permissions are
not silently repaired. Writes are atomic in the same directory and sync files and
directory. The OS releases advisory locks; the lock file is never unlinked.

Supported filesystem checks are local APFS/HFS on macOS and the ext-family, XFS,
Btrfs, tmpfs and overlayfs type allowlist on Linux, with working ACL inspection,
locking and sync. Unsupported filesystems/ACL semantics fail closed. These checks
are not a guarantee for every mount configuration. If the clock moves below the
greatest saved observation, state operations fail with exit 4 until time catches up
or the owner reviews it; time is not clamped to release budget.

## JSON and exits

Both `lnk safety status --json` and `lnk --json safety status` work. Resume likewise
accepts `--json` before the group or after the leaf. Successful status/resume JSON
goes to stdout with exactly these fields. This is a fresh synthetic local fixture,
not a live account response:

```json
{"admissions":0,"budget":100,"delay_seconds":5,"cooldown":"0001-01-01T00:00:00Z","block":""}
```

`admissions` is the current rolling count; `budget` and `delay_seconds` are the
selected config. `cooldown` is a UTC timestamp (the zero timestamp means unset).
`block` is empty, `auth` or `retry_after_overflow`.

| Exit | Meaning |
|---|---|
| 0 | Command success; safety status may still report blocked state |
| 1 | General command/API/body error |
| 2 | Usage, config, invalid flags or missing resume confirmation |
| 3 | Authentication/refusal/manual-review stop |
| 4 | Transport, response read/size, state I/O/permissions or clock failure |
| 5 | Budget, cooldown, Retry-After overflow or lock timeout |

Root diagnostic JSON goes to stderr, leaving stdout empty on these safety failures.
It always contains `error` (bounded generic message), `code` and `reason`.
Optional request fields are `method` (GET/POST/DELETE), `operation`
(read/graphql/normalized_read/post/delete) and numeric `status` (100..999), only
when available. `reason` is one of `http_status`, `redirect`, `transport`,
`response_read`, `response_size`, `invalid_json`, `graphql_errors`, `refusal`, or
fallback `usage`, `auth_stop`, `network_state`, `rate_limit`, `general`.
The status is diagnostic, not a new exit code. Missing resume confirmation yields:

```json
{"code":2,"error":"invalid arguments or configuration","reason":"usage"}
```

`--verbose` and `--debug` show bounded HTTP method/status on stderr. They never
unlock full bodies, headers, URLs or cookies. Explicit job/profile JSON still
contains the requested data; this is distinct from diagnostics.

## Session and API compatibility

The reference extraction used Android APK 4.1.1209. Current headers retain legacy
app version 4.1.1209/build 211700, Android-looking model/OS and Cronet text as
compatibility metadata. The actual transport is Go `net/http`, and authentication
reuses browser-session cookies. Typed tracking JSON fixes the duplicate `osVersion`
key (SDK string `36`); it does not prove a native Android login, current APK identity
or server acceptance of every header claim.

The reported “Other” session label is an opaque UI classification. It establishes
neither detection nor successful Android impersonation. Its cause is unknown here.
Only genuine official Android session evidence can establish an Android login;
even that does not authorize a third-party client. No attestation/device ID/TLS
spoofing, proxy rotation or new login to alter a label is proposed.

The separate audit observed mirror APK versions 4.1.1259 and beta 4.1.1261, without
proving Play rollout or publisher-signature authenticity. Six CLI query IDs absent
from beta DEX literals are drift evidence, not proof of server removal. Existing
working query IDs remain; the full newer catalogue has not been integrated. Any
replacement needs query-name, variables and response-schema evidence and separately
authorized bounded live verification. M3 help and offline fixtures prove local
behavior only, not current LinkedIn compatibility or native-session classification.

## Applications and agent permissions

The [CLI skill](../skill/SKILL.md#apply) and [README](../README.md#job) retain the
accepting/closed/unverified evidence contract and the isolated Lever/Workday verifier.
`job --check-apply` can contact a supported employer anonymously; its traffic is
separate from LinkedIn admission accounting. HTTP 200 or generic Apply text is not
application proof. Check deadlines and suitability separately.

`apply` prompts by default; `--dry-run` defaults false. Explicit dry-run previews
without submitting but still makes quota-counted live reads. `--confirm` skips the
prompt; `--no-input` or missing TTY without confirmation fails instead of prompting.
Agents must preview first and never add `--confirm` automatically. Only exact owner
authorization permits an application, saved-job/alert mutation or credential logout.
Local resume also requires explicit owner instruction after official-session review.
Bounded unattended reads require authorization and retain account risk; they are
not permission for unlimited polling. Never automatically obtain browser cookies.
