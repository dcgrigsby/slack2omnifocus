# slack2omnifocus — Design

**Date:** 2026-04-09
**Status:** Approved for implementation planning

## Goal

A personal Mac tool that turns Slack messages into OmniFocus inbox tasks when the user reacts to them with the 👀 (`:eyes:`) emoji. Runs locally via launchd every 5 minutes. No server, no webhooks, no daemon — just a Go binary invoked on a schedule.

## Trigger mechanism

- The user reacts to any Slack message (in any channel, group, or DM the user is a member of) with the 👀 emoji.
- Only the **user's own** 👀 reaction counts. Reactions by other users are ignored.
- After a message has been successfully turned into an OmniFocus task, slack2omnifocus removes the user's 👀 reaction as a visible confirmation and to prevent reprocessing.

The reaction itself acts as the primary work queue: presence of 👀 means "pending," absence of 👀 means "done or never marked."

### Why not Slack "Saved for later" / "Later"

The user's preferred approach was to use Slack's native "Saved for later" / "Later" feature. This is not feasible: Slack has no public API for it. Per Slack's own changelog, the legacy `stars.list` / `stars.add` / `stars.remove` methods are retired and no longer reflect the contents of a user's Later tab, and Slack states explicitly that "there are no direct APIs for Save it for Later to integrate with." Source: <https://docs.slack.dev/changelog/2023-07-its-later-already-for-stars-and-reminders/>.

## Architecture

```
┌─────────────────────┐
│   launchd (5 min)   │
└──────────┬──────────┘
           │
           │ sh -c 'set -a; . .env.local; exec slack2omnifocus poll'
           ▼
┌─────────────────────┐        ┌───────────────────┐
│  slack2omnifocus    │◄──────▶│   Slack Web API   │
│  (Go binary, poll)  │        │  (user token)     │
└──────────┬──────────┘        └───────────────────┘
           │
           │ osascript -e 'tell application "OmniFocus" to evaluate javascript "..."'
           ▼
┌─────────────────────┐
│     OmniFocus       │
│   (Inbox task)      │
└─────────────────────┘
```

Data flow for a single reacted message:

1. launchd fires `slack2omnifocus poll` every 300 seconds.
2. Binary checks `pgrep -x OmniFocus`. If not running, log and exit 0.
3. Binary calls Slack `auth.test` (first run only, cached for the process lifetime) to learn its own user ID.
4. Binary calls `reactions.list` (paginated) to get items the authenticated user has reacted to.
5. For each item, binary filters to messages where the **current user's** reactions include `eyes`.
6. Binary drops any `(channel, ts)` already recorded in `processed.txt` — safety net against duplicate processing.
7. For each remaining candidate, binary enriches with `users.info` (sender display name), `conversations.info` (channel name), and `chat.getPermalink` (deep link URL).
8. Binary builds an OmniJS snippet and runs it via `osascript`. If it succeeds (exit 0, parseable JSON response with a non-empty task ID), it appends the `(channel, ts)` to `processed.txt` (with fsync), then calls `reactions.remove` to strip the user's 👀.
9. Any failure at any point for a given message leaves the 👀 in place and is retried next poll.

## Slack side

### Authentication

- **Token type:** user OAuth token (`xoxp-...`), not a bot token. This is a personal tool; the token acts as the user.
- **Stored in:** `.env.local` at the repo root as `export SLACK_TOKEN=xoxp-...` (see Configuration below).

### Scopes required (User Token Scopes)

- `reactions:read` — list reactions the user has added
- `reactions:write` — remove reactions after processing
- `channels:history` — read message content in public channels
- `groups:history` — read message content in private channels
- `im:history` — read message content in DMs
- `mpim:history` — read message content in group DMs
- `channels:read` — resolve public channel IDs to names via `conversations.info`
- `groups:read` — resolve private channel IDs to names via `conversations.info`
- `im:read` — resolve DM IDs to names via `conversations.info`
- `mpim:read` — resolve group DM IDs to names via `conversations.info`
- `users:read` — resolve sender user IDs to display names via `users.info`

### Slack app setup — step-by-step (one-time, in the browser)

All steps happen at <https://api.slack.com/apps> while signed in to the target Slack workspace.

1. **Create the app.** Click **Create New App** → **From scratch**. Name it `slack2omnifocus`. Pick your workspace from the **Pick a workspace to develop your app** dropdown. Click **Create App**. You land on "Basic Information."

2. **Navigate to OAuth & Permissions.** Left sidebar → **Features** → **OAuth & Permissions**.

3. **Add user token scopes.** Scroll down to the **Scopes** section. Under **User Token Scopes** (not Bot Token Scopes — we don't use those), click **Add an OAuth Scope** once per scope and add all six listed above.

4. **Install the app to your workspace.** Scroll back to the top of the OAuth & Permissions page and click **Install to Workspace**. Slack shows a consent screen listing the scopes. Click **Allow**.

5. **Copy the user OAuth token.** Slack redirects back to the OAuth & Permissions page with a **User OAuth Token** starting with `xoxp-...`. Click **Copy**. Paste it into `.env.local` as `export SLACK_TOKEN=xoxp-...` (see Configuration below).

**Reinstalling after scope changes.** If you later add or remove any scope on the OAuth & Permissions page, Slack requires you to click **Install to Workspace** again. The `xoxp-...` token string itself does not change after reinstall; only the permissions it carries.

**No redirect URL needed.** This is a personal, single-workspace app installed via the "Install to Workspace" button on the app's own config page. You do not need a Redirect URL, a distribution flow, or App Directory review.

### Slack API usage details

- **`reactions.list`** with `user` omitted (defaults to the authenticated user) and `full=true` to get complete message bodies in the response. Paginate via cursor.
- **`auth.test`** at the start of every poll run to get the user's ID (cheap, and it doubles as a token-validity check).
- **`users.info`** to resolve the sender's user ID to a display name. Results cached in memory for the lifetime of a poll run (tasks from the same sender in one run share a lookup).
- **`conversations.info`** to resolve a channel ID to a channel name. Same caching strategy as users.info.
- **`chat.getPermalink`** to get the canonical `https://<workspace>.slack.com/archives/...` URL for the message. Handles threads correctly (permalink points at the reply, not the parent).
- **`reactions.remove`** with `name=eyes` and the message's `channel` + `timestamp` to strip the user's 👀 after success.

### Matching logic

A message is a candidate for processing iff all of the following are true:

- The message is present in `reactions.list` results for the authenticated user.
- One of the `reactions` on that message has `name == "eyes"`.
- That reaction's `users` array contains the authenticated user's own ID.
- The `(channel_id, message_ts)` pair is not already in `processed.txt`.

The "users array contains our own ID" check is what enforces "only the user's own 👀 counts." The API query is already scoped to reactions the authenticated user has made, but this assertion is defensive — it protects against future API quirks and makes the intent of the code explicit.

## OmniFocus side

### Running check

Before any `osascript` invocation, run `pgrep -x OmniFocus`. If exit code is non-zero, log `"OmniFocus not running, skipping"` and return from the poll with no Slack mutations. This is the behavior the user explicitly asked for — if OmniFocus isn't available, leave the 👀 reactions in place so they are retried next poll. The tool does **not** attempt to launch OmniFocus.

Do not use `tell application "OmniFocus" to ...` to check status, because it auto-launches the app as a side effect.

### Task creation via OmniJS

For each candidate message, build a Go payload:

```go
type taskPayload struct {
    Name string `json:"name"`
    Note string `json:"note"`
}
```

- `Name` is the first ~80 characters of the message text, single-lined (newlines replaced with spaces), with a trailing `…` if truncation happened.
- `Note` is a multi-line string:
  ```
  From @<sender_display_name> in #<channel_name>
  <permalink URL>

  <full message text, unmodified>
  ```

The payload is serialized with `json.Marshal`. Because JSON is a subset of JavaScript, the marshaled bytes drop directly into JS source as a valid object literal — this cleanly handles quotes, backslashes, newlines, and Unicode in Slack messages without any hand-rolled JS escaping.

The OmniJS snippet, with `%s` replaced by the marshaled JSON, is:

```javascript
(function(){
  var p = %s;
  var t = new Task(p.name, inbox.ending);
  t.note = p.note;
  return JSON.stringify({id: t.id.primaryKey});
})()
```

`inbox.ending` appends the new task to the bottom of the OmniFocus inbox (matches the "land in inbox" decision). Returning the new task's primary key gives a positive confirmation signal that slack2omnifocus can parse and verify.

### Invoking OmniJS via osascript

The Go binary wraps the OmniJS snippet in an AppleScript `evaluate javascript` call and runs it via `/usr/bin/osascript -e`:

```go
appleScript := fmt.Sprintf(
    `tell application "OmniFocus" to evaluate javascript %s`,
    applescriptQuoteString(jsSnippet),
)
cmd := exec.Command("/usr/bin/osascript", "-e", appleScript)
```

**`applescriptQuoteString` helper.** Escapes `\` → `\\` and `"` → `\"`, then wraps in double quotes. Lives in `internal/omnifocus/`, has unit tests, and is the only place in the codebase that knows AppleScript-level quoting. This mirrors the `runOsaScript` logic in the omnifocal project but lifts it into a standalone, tested helper so the call site stays clean.

### Failure detection

A task creation counts as successful only if **all** of the following hold:

- `osascript` exits 0.
- stderr is empty.
- stdout parses as JSON.
- The parsed object has a non-empty string `id` field.

Any deviation is a failure. On failure, slack2omnifocus:

1. Logs the failure with the offending `(channel_id, message_ts)` and the raw stdout/stderr.
2. Does **not** append to `processed.txt`.
3. Does **not** call `reactions.remove`.
4. Moves on to the next candidate.

Strict success criteria are intentional. OmniFocus can produce weird partial-success states (app still launching, document not loaded, sync conflict dialog open), and the cost of missing a task is higher than the cost of re-processing one. By requiring a round-trip confirmation, we refuse to remove the 👀 unless the task is definitely in OmniFocus.

### Out of scope for v1

No tags, no projects, no due dates, no defer dates, no flags. All tasks land naked in the inbox for manual triage. These are easy to add later — OmniJS supports `t.addTag(tagNamed("…"))`, `t.dueDate = new Date(...)`, and assignment to a specific project via `projectNamed("…")` — but the v1 scope is "inbox only."

## Runtime, configuration, and state

### Binary and commands

One Go binary with subcommands:

- `slack2omnifocus poll` — perform one poll cycle, then exit. This is what launchd runs.
- `slack2omnifocus doctor` — sanity-check token validity (`auth.test`), OmniFocus running state, config file readability, and state file writability. Useful for first-run setup and debugging.

There is no daemon mode and no long-running process. Each `poll` invocation sets up, does its work, and exits.

### launchd plist

Installed at `~/Library/LaunchAgents/com.slack2omnifocus.poll.plist`.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.slack2omnifocus.poll</string>

    <key>ProgramArguments</key>
    <array>
        <string>/bin/sh</string>
        <string>-c</string>
        <string>set -a; . /Users/dan/slack2omnifocus/.env.local; exec /usr/local/bin/slack2omnifocus poll</string>
    </array>

    <key>StartInterval</key>
    <integer>300</integer>

    <key>RunAtLoad</key>
    <true/>

    <key>StandardOutPath</key>
    <string>/tmp/slack2omnifocus.stdout.log</string>

    <key>StandardErrorPath</key>
    <string>/tmp/slack2omnifocus.stderr.log</string>
</dict>
</plist>
```

Installation: `launchctl load ~/Library/LaunchAgents/com.slack2omnifocus.poll.plist`.

Key choices:

- `StartInterval` of 300 seconds (5 minutes), matching the user's preference.
- `RunAtLoad` true — the first run fires immediately when the plist is loaded, so first-run feedback doesn't require waiting 5 minutes.
- No `KeepAlive` — we want the process to exit between runs.
- Stdout/stderr to `/tmp/slack2omnifocus.*.log`, matching the convention used in the omnifocal project.

### Configuration and token storage

**Pattern:** mirror the omnifocal project's `.env.local` + direnv approach exactly, for consistency across the user's personal tools.

Files in the repo:

```
slack2omnifocus/
├── .env.local       # gitignored, chmod 600, contains: export SLACK_TOKEN=xoxp-...
├── .envrc           # direnv: dotenv_if_exists .env.local
└── .gitignore       # ignores .env.local and .env
```

The Go binary reads the token via `os.Getenv("SLACK_TOKEN")`. It has no knowledge of `.env.local`, direnv, or any file-loading logic. This keeps Go code trivial and makes the tool behave identically whether it is invoked from your direnv shell or from launchd.

**launchd compatibility.** launchd does not run shell rc files, does not execute direnv, and does not source `.envrc`. The plist above wraps the binary in a tiny `sh -c` invocation that sources `.env.local` exactly the way direnv does internally:

1. `set -a` — automatically export every subsequent variable assignment.
2. `. /Users/dan/slack2omnifocus/.env.local` — POSIX-source the file.
3. `exec /usr/local/bin/slack2omnifocus poll` — replace the shell process with the Go binary so logs and exit codes propagate cleanly.

**Consistency check between the two invocation paths:**

| | Shell (manual) | launchd (scheduled) |
|---|---|---|
| How `.env.local` is loaded | direnv reads `.envrc` → `dotenv_if_exists .env.local` | `sh -c 'set -a; . .env.local; …'` |
| What the Go binary sees | `SLACK_TOKEN` in `os.Environ()` | `SLACK_TOKEN` in `os.Environ()` |
| Go code path | Identical | Identical |

**Security.** The token is plaintext on disk at `.env.local`, protected by `chmod 600` (readable only by the user). This is the same protection model used by most Mac apps for their credentials. `.env.local` is gitignored and never committed. Not in the launchd plist (plists are world-readable by the user), not in env vars baked anywhere, not in the repo.

**Explicitly rejected alternatives:**

- **macOS Keychain.** User preference; adds setup ceremony (`security add-generic-password`) without meaningful additional protection for a single-user personal tool.
- **Token in launchd EnvironmentVariables plist key.** Would put the token in `~/Library/LaunchAgents/*.plist`, which is user-readable and inspected by many Mac tools.
- **`--env-file` flag read by Go.** Cleaner plist but adds a dotenv parser (code or dependency) to the Go side and creates two code paths for the same file format (direnv for shell, Go parser for launchd). The `sh -c` wrapper keeps the Go side zero-code.

### State file for deduplication

Location: `~/.local/state/slack2omnifocus/processed.txt`.

Format: one entry per line, `<channel_id>:<message_ts>`, e.g.:

```
C024BE91L:1712345678.123456
D03TRU78K:1712345901.456789
```

Access pattern:

1. On startup, `poll` reads the whole file into an in-memory `map[string]struct{}` set. At expected usage (tens to hundreds of entries over a tool's lifetime), this is trivial — the whole file is well under a megabyte even after years of use.
2. Candidates found in `reactions.list` are looked up in the set and skipped if present.
3. After a successful OmniFocus task creation, the new entry is **appended** to the file, fsynced, and only then is `reactions.remove` called.

**Ordering matters.** The append-and-fsync happens before `reactions.remove` specifically so that if removal fails (Slack rate limit, transient network, whatever), the next poll still sees the message as already-processed and will retry only the reaction removal, not re-create the task.

No pruning in v1. At ~30 bytes per entry the file is effectively infinite for human-scale usage.

### Project layout

```
slack2omnifocus/
├── cmd/
│   └── slack2omnifocus/
│       └── main.go                     # CLI entry, subcommand dispatch
├── internal/
│   ├── config/                         # SLACK_TOKEN env var load + validation
│   ├── slack/                          # Thin wrapper over slack-go/slack
│   ├── omnifocus/                      # Running check, OmniJS snippet builder,
│   │                                   #   osascript runner, AppleScript quoting
│   ├── state/                          # processed.txt read/append with fsync
│   └── poll/                           # Orchestration: wire the pieces together
├── launchd/
│   └── com.slack2omnifocus.poll.plist
├── docs/
│   └── plans/
│       └── 2026-04-09-slack2omnifocus-design.md
├── .env.local                          # gitignored
├── .envrc                              # direnv config
├── .gitignore
├── go.mod
├── go.sum
└── README.md
```

### Dependencies

- **`github.com/slack-go/slack`** — the de facto community Slack SDK for Go. Actively maintained, covers every endpoint we need (`reactions.list`, `reactions.remove`, `users.info`, `conversations.info`, `chat.getPermalink`, `auth.test`), and includes pagination helpers and built-in rate-limit handling.
- Everything else is Go standard library: `os/exec`, `encoding/json`, `log/slog`, `os`, `bufio`.

### Logging

- `log/slog` (stdlib) with a text handler — easy to eyeball in `/tmp/slack2omnifocus.*.log`.
- INFO for normal operations, ERROR for failures.
- Every poll logs: start, OmniFocus running state, candidates found, per-candidate outcome (success/failure with reason), and a one-line summary.
- No log rotation in v1. At expected volume the logs grow by well under a KB per day.

## Testability

Each `internal/` package is designed to be unit-testable in isolation without a real Slack token or a real OmniFocus install.

- **`internal/omnifocus/applescriptQuoteString`** — table-driven tests, including strings with backslashes, quotes, newlines, and Unicode.
- **`internal/omnifocus` snippet builder** — given a known payload, asserts the produced OmniJS string against a golden file. Plus a parser-level assertion that the produced JS is parseable (optional — can use `goja` or just a regex sanity check).
- **`internal/state`** — temp-dir test that writes entries, reads them back, and verifies fsync behavior by inspecting file modtime + size after append.
- **`internal/config`** — happy-path test (env var set), error-path test (env var missing).
- **`internal/slack`** — tested against `httptest.Server` returning canned Slack API response JSON. No real token needed.
- **`internal/poll` orchestration** — end-to-end test with the Slack client pointed at an httptest server and a fake `omnifocus.Runner` interface that records calls instead of shelling to osascript. Verifies the happy path, the "OmniFocus not running" path, the "OmniFocus fails" path, and the dedup behavior.

## First-run checklist (for the user, once implemented)

1. Create the Slack app and install it per the step-by-step above. Copy the `xoxp-...` token.
2. Clone the repo to `/Users/dan/slack2omnifocus/` (or adjust paths in the plist).
3. Create `.env.local` in the repo root with `export SLACK_TOKEN=xoxp-...`, then `chmod 600 .env.local`.
4. `go build -o slack2omnifocus ./cmd/slack2omnifocus && sudo cp slack2omnifocus /usr/local/bin/`.
5. `slack2omnifocus doctor` — verify token validity, OmniFocus running state, and file permissions.
6. `slack2omnifocus poll` — manual first run, confirm at least one 👀-reacted message gets turned into an inbox task.
7. `cp launchd/com.slack2omnifocus.poll.plist ~/Library/LaunchAgents/ && launchctl load ~/Library/LaunchAgents/com.slack2omnifocus.poll.plist`.
8. React to a test message with 👀. Within 5 minutes (or immediately on `RunAtLoad`), it appears in the OmniFocus inbox and the 👀 is removed.

## Open questions

None — design approved. Ready to hand off to `writing-plans` for a detailed implementation plan.
