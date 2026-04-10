# slack2omnifocus

Turn Slack messages into OmniFocus inbox tasks by reacting to them with any emoji.

> **Note:** This project was built with [Claude Code](https://claude.ai/code) and is provided as-is with no warranty. See [LICENSE](LICENSE) for details.

## How it works

You react to a Slack message with your chosen emoji (e.g. :eyes:), and
slack2omnifocus does the rest. A launchd job polls every 5 minutes, finds
messages you've reacted to, and creates an OmniFocus inbox task for each one
containing the message text, sender, channel, and a permalink back to the
original conversation. It then removes the reaction as confirmation. If
OmniFocus isn't running, reactions stay put and get retried on the next poll.

## Prerequisites

- macOS with OmniFocus installed
- Go 1.22+ for building
- A Slack workspace where you can create apps

## Create a Slack app

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
5. Copy the **User OAuth Token** (starts with `xoxp-...`).

If you later change scopes, Slack will require you to click **Install to
Workspace** again to update the token's permissions.

## Build and install

```bash
go build -o slack2omnifocus ./cmd/slack2omnifocus
sudo cp slack2omnifocus /usr/local/bin/
```

## Usage

```bash
slack2omnifocus poll   --token xoxp-... --reaction eyes
slack2omnifocus doctor --token xoxp-... --reaction eyes
```

- **`poll`** runs one poll cycle: fetch reactions, create tasks, remove
  reactions.
- **`doctor`** validates your setup — checks token auth, OmniFocus status,
  and state file.

Both commands require `--token` and `--reaction`.

## Run with launchd

Copy the template plist into your LaunchAgents folder and edit it with your
real token and reaction name:

```bash
cp launchd/com.slack2omnifocus.poll.plist ~/Library/LaunchAgents/
# Edit ~/Library/LaunchAgents/com.slack2omnifocus.poll.plist
# Replace YOUR_TOKEN_HERE and YOUR_REACTION_HERE with real values
launchctl load ~/Library/LaunchAgents/com.slack2omnifocus.poll.plist
```

The plist has `RunAtLoad` set, so the first poll runs immediately on load.
After that it runs every 300 seconds (5 minutes).

To unload:

```bash
launchctl unload ~/Library/LaunchAgents/com.slack2omnifocus.poll.plist
```

## Multiple workspaces

To watch more than one Slack workspace, create additional plist files — one
per workspace. Each plist needs a unique `Label` value (e.g.
`com.slack2omnifocus.poll.work`) along with its own `--token` and
`--reaction` values. Each instance gets its own state file automatically,
keyed to a hash of the token.

## Troubleshooting

- **Logs:** Check `/tmp/slack2omnifocus.stderr.log` for errors captured by
  launchd.
- **Nothing happens after reacting:** Confirm OmniFocus is running, then
  run `slack2omnifocus doctor --token ... --reaction ...` to check your
  setup. If doctor is clean, run `slack2omnifocus poll` manually and inspect
  its output.
- **Token errors:** Make sure you copied the **User OAuth Token**
  (`xoxp-...`), not a Bot Token (`xoxb-...`). The User OAuth Token is at
  the top of the OAuth & Permissions page.
- **`missing_scope` in logs:** Add the missing scope on the app's OAuth &
  Permissions page and click **Install to Workspace** again to refresh the
  token.

## License

Apache 2.0. See [LICENSE](LICENSE) for details.
