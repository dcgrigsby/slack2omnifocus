# Generalize slack2omnifocus CLI

**Date:** 2026-04-10
**Status:** Approved

## Goal

Make slack2omnifocus usable by anyone, not just the original author. Move
all configuration from environment variables to CLI flags, isolate state
per Slack workspace, add an Apache 2.0 license, and rewrite documentation
for a general audience.

## CLI Interface

Both `poll` and `doctor` subcommands accept the same two required flags:

```
slack2omnifocus poll --token xoxp-... --reaction eyes
slack2omnifocus doctor --token xoxp-... --reaction eyes
```

- `--token` (required): Slack user token (`xoxp-...`).
- `--reaction` (required): Slack reaction name without colons (e.g.,
  `eyes`, `white_check_mark`).
- Missing either flag prints usage and exits with code 2.

Each subcommand uses its own `flag.FlagSet` from the Go standard library.
No third-party CLI framework.

## Config Package Changes

`internal/config/config.go` stops reading the `SLACK_TOKEN` environment
variable. Instead it accepts a struct with `Token` and `Reaction` fields,
populated by `main.go` from parsed flags.

Validation:
- Token must be non-empty and start with `xoxp-`.
- Reaction must be non-empty.

The reaction name (currently hardcoded as `"eyes"` in
`internal/slack/client.go`) becomes a config value threaded through the
call chain.

## State File Isolation

**Current path:** `~/.local/state/slack2omnifocus/processed.txt`

**New path:** `~/.local/state/slack2omnifocus/processed-<hash>.txt`

`<hash>` is the first 8 hex characters of the SHA-256 digest of the
token. Each Slack workspace gets its own state file automatically. No
migration is needed; reprocessing a previously-handled message is harmless.

## Files Removed

| File | Reason |
|------|--------|
| `.env.local.example` | Token now passed via `--token` flag |
| `.envrc` | direnv no longer needed |
| `.env.local` entry in `.gitignore` | No longer relevant |

## Files Added

| File | Content |
|------|---------|
| `LICENSE` | Apache License 2.0 |

## Launchd Plist

The existing plist becomes a generic template. The `sh -c` wrapper that
sourced `.env.local` is replaced with a direct invocation:

```xml
<array>
    <string>/usr/local/bin/slack2omnifocus</string>
    <string>poll</string>
    <string>--token</string>
    <string>YOUR_TOKEN_HERE</string>
    <string>--reaction</string>
    <string>YOUR_REACTION_HERE</string>
</array>
```

Install path uses `/usr/local/bin` as the conventional location. Users
adjust to match their setup.

## README

Complete rewrite for a general audience. Includes a prominent notice that
the project was built with Claude Code and comes with no warranty.

Sections:
1. What it does (brief description)
2. Disclaimer (built with Claude Code, no warranty)
3. Prerequisites (macOS, OmniFocus, Slack user token with required scopes)
4. Installation (`go install` or build from source)
5. Usage (CLI flags, examples)
6. Launchd setup (copy template, edit values, load)
7. Multiple workspaces (add more plist files with different flags)
8. Troubleshooting

## Testing

Existing tests update to pass reaction as a parameter instead of relying
on a hardcoded value. No new test files needed; the scope of changes is
small enough that updating existing tests covers it.

## What Does Not Change

- Core polling logic (`internal/poll/poll.go`)
- Slack API client structure (`internal/slack/`)
- OmniFocus integration (`internal/omnifocus/`)
- State file format (still `channel:ts` lines, one per entry)
- Deduplication and fsync-before-reaction-removal ordering
