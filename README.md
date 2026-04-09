# slack2omnifocus

Turn Slack messages into OmniFocus inbox tasks by reacting to them with 👀.

A personal macOS tool that polls your Slack reaction history every 5 minutes
via launchd. When it sees a message you've reacted to with 👀, it creates
an inbox task in OmniFocus containing the message text, sender, channel,
and a deep link back to the original conversation — then removes your 👀
as confirmation. If OmniFocus isn't running, your reactions stay put and
get retried on the next poll.

## Prerequisites

- macOS with OmniFocus installed
- Go 1.22+ for building
- [direnv](https://direnv.net/) (optional, for the manual-invocation workflow)
- A Slack account where you can install personal apps

## Setup

### 1. Create a Slack app and get a user token

Walk through these steps once at <https://api.slack.com/apps> while signed
in to your workspace:

1. Click **Create New App** → **From scratch**. Name it `slack2omnifocus`,
   pick your workspace, click **Create App**.
2. In the left sidebar under **Features**, click **OAuth & Permissions**.
3. Scroll to **Scopes** → **User Token Scopes** (NOT Bot Token Scopes).
   Click **Add an OAuth Scope** once per scope and add:
   - `reactions:read`
   - `reactions:write`
   - `channels:history`
   - `groups:history`
   - `im:history`
   - `mpim:history`
   - `channels:read`
   - `groups:read`
   - `im:read`
   - `mpim:read`
   - `users:read`
4. Scroll back to the top and click **Install to Workspace** → **Allow**.
5. Copy the **User OAuth Token** (starts with `xoxp-…`).

If you later change scopes, Slack will require you to click **Install to
Workspace** again to update the token's permissions.

### 2. Put the token in `.env.local`

In the repo root:

```bash
cp .env.local.example .env.local
chmod 600 .env.local
# Edit .env.local and replace xoxp-REPLACE-ME with your real token
```

If you use direnv: `direnv allow` in the repo root so `SLACK_TOKEN` is
auto-loaded into your shell.

### 3. Build and install the binary

```bash
go build -o slack2omnifocus ./cmd/slack2omnifocus
sudo cp slack2omnifocus /usr/local/bin/slack2omnifocus
```

### 4. Run `doctor` to verify

```bash
slack2omnifocus doctor
```

You should see checkmarks for config, auth, OmniFocus running, and state
file. If OmniFocus isn't running that's a warning, not a failure.

### 5. Manual smoke test

React to any Slack message with 👀, then run:

```bash
slack2omnifocus poll
```

The message should appear in your OmniFocus inbox within a second or two,
and your 👀 reaction should disappear from the Slack message.

### 6. Install the launchd job

```bash
cp launchd/com.slack2omnifocus.poll.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.slack2omnifocus.poll.plist
```

The plist has `RunAtLoad` set, so the first run happens immediately on
load. After that it runs every 300 seconds.

To uninstall:

```bash
launchctl unload ~/Library/LaunchAgents/com.slack2omnifocus.poll.plist
rm ~/Library/LaunchAgents/com.slack2omnifocus.poll.plist
```

## How it works

See `docs/plans/2026-04-09-slack2omnifocus-design.md` for the full design,
including why we don't use Slack's "Saved for later" feature (no API),
why we use `pgrep` instead of `tell application` for the running check
(no auto-launch), and why the `.env.local` + `sh -c` wrapper pattern
matches the sibling `omnifocal` project.

## Troubleshooting

- **`/tmp/slack2omnifocus.stderr.log`** is where launchd captures stderr.
  Check it if tasks stop appearing.
- **Nothing happens after I react:** confirm OmniFocus is running, then
  run `slack2omnifocus doctor`. If doctor is clean, run `slack2omnifocus
  poll` manually and look at its output.
- **"SLACK_TOKEN environment variable is not set":** either direnv didn't
  load your `.env.local`, or you didn't chmod it 600 and create it.
- **"SLACK_TOKEN does not look like a user token":** you probably copied
  a Bot Token (`xoxb-…`) instead of a User Token (`xoxp-…`). Reinstall
  the app and copy the User OAuth Token from the top of the OAuth &
  Permissions page.
- **`missing_scope` in the stderr log:** slack2omnifocus called a Slack
  API method whose scope your token doesn't have. Add the missing scope
  on the app's OAuth & Permissions page and click **Install to
  Workspace** again to refresh the token.
