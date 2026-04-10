# Generalize CLI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Move all configuration from environment variables to CLI flags (`--token`, `--reaction`), isolate state per workspace, add Apache 2.0 license, and rewrite docs for general use.

**Architecture:** Config changes from reading env vars to accepting caller-supplied values. The Slack client stores the reaction name and uses it instead of the hardcoded `"eyes"`. State file path includes a hash of the token for workspace isolation. main.go parses flags and wires everything together.

**Tech Stack:** Go standard library (`flag`, `crypto/sha256`, `encoding/hex`)

---

### Task 1: Update config package to accept direct values

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Update tests to use New(token, reaction) instead of Load()**

Replace all tests. The new `New` function takes token and reaction as arguments and validates both.

```go
package config

import (
	"strings"
	"testing"
)

func TestNew_happyPath(t *testing.T) {
	cfg, err := New("xoxp-test-token-123", "eyes")
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	if cfg.Token != "xoxp-test-token-123" {
		t.Errorf("Token = %q, want %q", cfg.Token, "xoxp-test-token-123")
	}
	if cfg.Reaction != "eyes" {
		t.Errorf("Reaction = %q, want %q", cfg.Reaction, "eyes")
	}
}

func TestNew_emptyToken(t *testing.T) {
	_, err := New("", "eyes")
	if err == nil {
		t.Fatal("New() with empty token returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error message does not mention token: %v", err)
	}
}

func TestNew_wrongTokenPrefix(t *testing.T) {
	_, err := New("xoxb-bot-token-should-be-rejected", "eyes")
	if err == nil {
		t.Fatal("New() with xoxb- token returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "xoxp-") {
		t.Errorf("error message does not mention xoxp-: %v", err)
	}
}

func TestNew_emptyReaction(t *testing.T) {
	_, err := New("xoxp-test-token-123", "")
	if err == nil {
		t.Fatal("New() with empty reaction returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "reaction") {
		t.Errorf("error message does not mention reaction: %v", err)
	}
}

func TestNew_trimsWhitespace(t *testing.T) {
	cfg, err := New("  xoxp-padded-token  \n", "  eyes  ")
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	if cfg.Token != "xoxp-padded-token" {
		t.Errorf("Token = %q, want trimmed %q", cfg.Token, "xoxp-padded-token")
	}
	if cfg.Reaction != "eyes" {
		t.Errorf("Reaction = %q, want trimmed %q", cfg.Reaction, "eyes")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/dan/slack2omnifocus && go test ./internal/config/ -v`
Expected: FAIL — `New` is not defined, `Config.Reaction` does not exist.

**Step 3: Implement config.New**

Replace `internal/config/config.go` entirely:

```go
// Package config validates slack2omnifocus runtime configuration.
package config

import (
	"errors"
	"fmt"
	"strings"
)

// Config holds validated runtime configuration.
type Config struct {
	// Token is a Slack user OAuth token (xoxp-...).
	Token string
	// Reaction is the Slack reaction name to watch (e.g. "eyes").
	Reaction string
}

// New validates and returns a Config from the given values.
func New(token, reaction string) (Config, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Config{}, errors.New("token is required (pass --token xoxp-...)")
	}
	if !strings.HasPrefix(token, "xoxp-") {
		return Config{}, fmt.Errorf("token does not look like a user token: expected xoxp- prefix, got %q…", safePrefix(token))
	}
	reaction = strings.TrimSpace(reaction)
	if reaction == "" {
		return Config{}, errors.New("reaction is required (pass --reaction <name>)")
	}
	return Config{Token: token, Reaction: reaction}, nil
}

// safePrefix returns the first few characters of s for use in error messages,
// without leaking the whole token.
func safePrefix(s string) string {
	const n = 5
	if len(s) <= n {
		return s
	}
	return s[:n]
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/dan/slack2omnifocus && go test ./internal/config/ -v`
Expected: All 5 tests PASS.

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "Replace config.Load with config.New(token, reaction)"
```

---

### Task 2: Make Slack Client accept and use configurable reaction name

**Files:**
- Modify: `internal/slack/client.go`
- Modify: `internal/slack/client_test.go`

**Step 1: Update tests to pass reaction to constructors and rename methods**

In `client_test.go`:

1. Update `newForTest` to pass a reaction parameter:
```go
func newForTest(t *testing.T, fakeURL string) *Client {
	t.Helper()
	c, err := NewWithURL("xoxp-test-token", fakeURL, "eyes")
	if err != nil {
		t.Fatalf("NewWithURL error: %v", err)
	}
	return c
}
```

2. Rename all `ListEyesReactions` calls to `ListReactions`.

3. Rename all `RemoveEyesReaction` calls to `RemoveReaction`.

4. Add a test verifying that a custom reaction name is used:
```go
func TestListReactions_usesConfiguredReactionName(t *testing.T) {
	srv := (&fakeSlackServer{
		handlers: map[string]http.HandlerFunc{
			"/reactions.list": func(w http.ResponseWriter, r *http.Request) {
				jsonOK(w, map[string]any{
					"items": []map[string]any{
						{
							"type":    "message",
							"channel": "C1",
							"message": map[string]any{
								"type": "message",
								"ts":   "1.0",
								"user": "UAUTHOR",
								"text": "flagged",
								"reactions": []map[string]any{
									{"name": "flag", "count": 1, "users": []string{"USELF"}},
								},
							},
						},
					},
					"response_metadata": map[string]any{"next_cursor": ""},
				})
			},
		},
	}).start(t)

	c, err := NewWithURL("xoxp-test-token", srv, "flag")
	if err != nil {
		t.Fatalf("NewWithURL error: %v", err)
	}
	items, err := c.ListReactions(context.Background(), "USELF")
	if err != nil {
		t.Fatalf("ListReactions error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
}

func TestRemoveReaction_usesConfiguredReactionName(t *testing.T) {
	var gotName string
	srv := (&fakeSlackServer{
		handlers: map[string]http.HandlerFunc{
			"/reactions.remove": func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				gotName = r.FormValue("name")
				jsonOK(w, map[string]any{})
			},
		},
	}).start(t)

	c, err := NewWithURL("xoxp-test-token", srv, "white_check_mark")
	if err != nil {
		t.Fatalf("NewWithURL error: %v", err)
	}
	if err := c.RemoveReaction(context.Background(), "C7", "42.17"); err != nil {
		t.Fatalf("RemoveReaction error: %v", err)
	}
	if gotName != "white_check_mark" {
		t.Errorf("name = %q, want %q", gotName, "white_check_mark")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/dan/slack2omnifocus && go test ./internal/slack/ -v`
Expected: FAIL — wrong signatures, missing methods.

**Step 3: Update client.go**

Changes to `internal/slack/client.go`:

1. Add `reaction` field to `Client` struct:
```go
type Client struct {
	api      *slackgo.Client
	reaction string

	userNameCache    map[string]string
	channelNameCache map[string]string
}
```

2. Update `New` to accept reaction:
```go
func New(token, reaction string) *Client {
	return &Client{
		api:              slackgo.New(token),
		reaction:         reaction,
		userNameCache:    map[string]string{},
		channelNameCache: map[string]string{},
	}
}
```

3. Update `NewWithURL` to accept reaction:
```go
func NewWithURL(token, apiURL, reaction string) (*Client, error) {
	return &Client{
		api:              slackgo.New(token, slackgo.OptionAPIURL(apiURL)),
		reaction:         reaction,
		userNameCache:    map[string]string{},
		channelNameCache: map[string]string{},
	}, nil
}
```

4. Rename `ListEyesReactions` → `ListReactions` and replace hardcoded `"eyes"` with `c.reaction`:
```go
func (c *Client) ListReactions(ctx context.Context, selfUserID string) ([]ReactedMessage, error) {
```
At line 88, change `r.Name != "eyes"` to `r.Name != c.reaction`.

5. Rename `RemoveEyesReaction` → `RemoveReaction` and replace hardcoded `"eyes"` with `c.reaction`:
```go
func (c *Client) RemoveReaction(ctx context.Context, channel, ts string) error {
	err := c.api.RemoveReactionContext(ctx, c.reaction, slackgo.NewRefToMessage(channel, ts))
```

6. Update the `ReactedMessage` doc comment to not mention 👀 specifically.

**Step 4: Run tests to verify they pass**

Run: `cd /Users/dan/slack2omnifocus && go test ./internal/slack/ -v`
Expected: All tests PASS.

**Step 5: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "Make reaction name configurable in Slack client"
```

---

### Task 3: Update PollAdapter and poll.SlackClient interface

**Files:**
- Modify: `internal/slack/poll_adapter.go`
- Modify: `internal/poll/poll.go`
- Modify: `internal/poll/poll_test.go`

**Step 1: Update poll_test.go fakeSlack to use new method names**

In `internal/poll/poll_test.go`, rename `fakeSlack` methods:
- `ListEyesReactions` → `ListReactions`
- `RemoveEyesReaction` → `RemoveReaction`

**Step 2: Update the SlackClient interface in poll.go**

In `internal/poll/poll.go`, rename interface methods:
```go
type SlackClient interface {
	AuthTest(ctx context.Context) (string, error)
	ListReactions(ctx context.Context, selfUserID string) ([]SlackMessage, error)
	DisplayName(ctx context.Context, userID string) (string, error)
	ChannelName(ctx context.Context, channelID string) (string, error)
	FormatText(ctx context.Context, text string) string
	Permalink(ctx context.Context, channel, ts string) (string, error)
	RemoveReaction(ctx context.Context, channel, ts string) error
}
```

Update calls in `Run` (line 66) and `handleMessage` (lines 107, 150):
- `slack.ListEyesReactions(...)` → `slack.ListReactions(...)`
- `slack.RemoveEyesReaction(...)` → `slack.RemoveReaction(...)`

Update the log/error messages mentioning "eyes" to be generic:
- Line 107 log: `"retrying reaction removal"` (already generic)
- Line 150 error: `"remove reaction"` (instead of `"remove eyes reaction"`)

Update the package doc comment at the top of poll.go to not hardcode 👀.

**Step 3: Update PollAdapter in poll_adapter.go**

Rename methods to match:
```go
func (a *PollAdapter) ListReactions(ctx context.Context, selfUserID string) ([]poll.SlackMessage, error) {
	items, err := a.Client.ListReactions(ctx, selfUserID)
```

```go
func (a *PollAdapter) RemoveReaction(ctx context.Context, channel, ts string) error {
	return a.Client.RemoveReaction(ctx, channel, ts)
}
```

**Step 4: Run all tests to verify they pass**

Run: `cd /Users/dan/slack2omnifocus && go test ./... -v`
Expected: All tests PASS.

**Step 5: Commit**

```bash
git add internal/slack/poll_adapter.go internal/poll/poll.go internal/poll/poll_test.go
git commit -m "Rename reaction methods to be generic across codebase"
```

---

### Task 4: Add token-hashed state file path

**Files:**
- Modify: `cmd/slack2omnifocus/main.go`

**Step 1: Write a test for statePath hashing**

Add a test file `cmd/slack2omnifocus/main_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestStatePath_includesTokenHash(t *testing.T) {
	p1 := statePath("/fake/home", "xoxp-token-aaa")
	p2 := statePath("/fake/home", "xoxp-token-bbb")

	if p1 == p2 {
		t.Errorf("different tokens should produce different paths, both got %q", p1)
	}
	if !strings.HasPrefix(p1, "/fake/home/.local/state/slack2omnifocus/processed-") {
		t.Errorf("unexpected path prefix: %q", p1)
	}
	if !strings.HasSuffix(p1, ".txt") {
		t.Errorf("path should end with .txt: %q", p1)
	}
}

func TestStatePath_sameTokenSamePath(t *testing.T) {
	p1 := statePath("/fake/home", "xoxp-same-token")
	p2 := statePath("/fake/home", "xoxp-same-token")
	if p1 != p2 {
		t.Errorf("same token should produce same path: %q vs %q", p1, p2)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/dan/slack2omnifocus && go test ./cmd/slack2omnifocus/ -v -run TestStatePath`
Expected: FAIL — `statePath` is not defined.

**Step 3: Implement statePath and update defaultStatePath**

In `cmd/slack2omnifocus/main.go`, replace `defaultStatePath()` with:

```go
import (
	"crypto/sha256"
	"encoding/hex"
)

func statePath(home, token string) string {
	h := sha256.Sum256([]byte(token))
	tag := hex.EncodeToString(h[:])[:8]
	return filepath.Join(home, ".local", "state", "slack2omnifocus", "processed-"+tag+".txt")
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/dan/slack2omnifocus && go test ./cmd/slack2omnifocus/ -v -run TestStatePath`
Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/slack2omnifocus/main.go cmd/slack2omnifocus/main_test.go
git commit -m "Add token-hashed state file path for workspace isolation"
```

---

### Task 5: Wire up CLI flag parsing in main.go

**Files:**
- Modify: `cmd/slack2omnifocus/main.go`

**Step 1: Rewrite main.go with flag parsing**

Replace the entire file. Key changes:
- Each subcommand gets its own `flag.FlagSet` with `--token` and `--reaction` (both required)
- `runPoll` and `runDoctor` accept `config.Config` instead of calling `config.Load()`
- `usage()` updated for the new interface
- `statePath()` uses the token hash from Task 4

```go
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/dcgrigsby/slack2omnifocus/internal/config"
	"github.com/dcgrigsby/slack2omnifocus/internal/omnifocus"
	"github.com/dcgrigsby/slack2omnifocus/internal/poll"
	"github.com/dcgrigsby/slack2omnifocus/internal/slack"
	"github.com/dcgrigsby/slack2omnifocus/internal/state"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "poll":
		cfg := parseFlags("poll", os.Args[2:])
		if err := runPoll(cfg); err != nil {
			slog.Error("poll failed", slog.Any("error", err))
			os.Exit(1)
		}
	case "doctor":
		cfg := parseFlags("doctor", os.Args[2:])
		if err := runDoctor(cfg); err != nil {
			slog.Error("doctor failed", slog.Any("error", err))
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func parseFlags(cmd string, args []string) config.Config {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	token := fs.String("token", "", "Slack user OAuth token (xoxp-...)")
	reaction := fs.String("reaction", "", "Slack reaction name to watch (e.g. eyes, white_check_mark)")
	fs.Parse(args)

	cfg, err := config.New(*token, *reaction)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n", err)
		fs.Usage()
		os.Exit(2)
	}
	return cfg
}

func usage() {
	fmt.Fprint(os.Stderr, `slack2omnifocus — turn reacted Slack messages into OmniFocus inbox tasks

Usage:
  slack2omnifocus poll   --token xoxp-... --reaction eyes
  slack2omnifocus doctor --token xoxp-... --reaction eyes
  slack2omnifocus help

Flags:
  --token      Slack user OAuth token (starts with xoxp-)
  --reaction   Slack reaction name to watch (e.g. eyes, white_check_mark)
`)
}

func runPoll(cfg config.Config) error {
	slackClient := slack.New(cfg.Token, cfg.Reaction)
	adapter := &slack.PollAdapter{Client: slackClient}
	runner := omnifocus.OsascriptRunner{}

	sp, err := defaultStatePath(cfg.Token)
	if err != nil {
		return err
	}
	store, err := state.Open(sp)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	return poll.Run(ctx, adapter, runner, store, omnifocus.IsRunning)
}

func runDoctor(cfg config.Config) error {
	failures := 0

	fmt.Println("✓ config validated (token prefix OK, reaction set)")

	client := slack.New(cfg.Token, cfg.Reaction)
	userID, err := client.AuthTest(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ slack auth.test:", err)
		failures++
	} else {
		fmt.Printf("✓ Slack auth.test OK (user_id=%s)\n", userID)
	}

	if omnifocus.IsRunning() {
		fmt.Println("✓ OmniFocus is running")
	} else {
		fmt.Println("⚠ OmniFocus is NOT running — poll will skip until it is")
	}

	sp, err := defaultStatePath(cfg.Token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ state path:", err)
		failures++
	} else if _, err := state.Open(sp); err != nil {
		fmt.Fprintln(os.Stderr, "✗ state file:", err)
		failures++
	} else {
		fmt.Printf("✓ State file writable at %s\n", sp)
	}

	if failures > 0 {
		return fmt.Errorf("%d doctor check(s) failed", failures)
	}
	return nil
}

func defaultStatePath(token string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home dir: %w", err)
	}
	return statePath(home, token), nil
}

func statePath(home, token string) string {
	h := sha256.Sum256([]byte(token))
	tag := hex.EncodeToString(h[:])[:8]
	return filepath.Join(home, ".local", "state", "slack2omnifocus", "processed-"+tag+".txt")
}
```

**Step 2: Run all tests**

Run: `cd /Users/dan/slack2omnifocus && go test ./... -v`
Expected: All tests PASS (including the statePath tests from Task 4).

**Step 3: Commit**

```bash
git add cmd/slack2omnifocus/main.go
git commit -m "Wire up --token and --reaction CLI flags in main.go"
```

---

### Task 6: Remove env-var files and update .gitignore

**Files:**
- Delete: `.env.local.example`
- Delete: `.envrc`
- Modify: `.gitignore`

**Step 1: Remove files and update .gitignore**

Delete `.env.local.example` and `.envrc`.

Update `.gitignore` to remove the env-related lines. New contents:

```
# Isolated implementation worktrees
.worktrees/

# Build output
/slack2omnifocus

# Launchd log files (written by the daemon, not tracked)
/tmp/slack2omnifocus.*.log

# Editor cruft
.DS_Store
```

**Step 2: Run all tests to make sure nothing broke**

Run: `cd /Users/dan/slack2omnifocus && go test ./... -v`
Expected: All tests PASS.

**Step 3: Commit**

```bash
git rm .env.local.example .envrc
git add .gitignore
git commit -m "Remove env-var config files; token now passed via --token flag"
```

---

### Task 7: Update launchd plist to use CLI flags

**Files:**
- Modify: `launchd/com.slack2omnifocus.poll.plist`

**Step 1: Replace plist with generic template**

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.slack2omnifocus.poll</string>

    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/slack2omnifocus</string>
        <string>poll</string>
        <string>--token</string>
        <string>YOUR_TOKEN_HERE</string>
        <string>--reaction</string>
        <string>YOUR_REACTION_HERE</string>
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

**Step 2: Commit**

```bash
git add launchd/com.slack2omnifocus.poll.plist
git commit -m "Update launchd plist to use --token and --reaction flags"
```

---

### Task 8: Add Apache 2.0 license

**Files:**
- Create: `LICENSE`

**Step 1: Create LICENSE file**

Write the standard Apache License 2.0 text with copyright line:
`Copyright 2026 Dan Grigsby`

**Step 2: Commit**

```bash
git add LICENSE
git commit -m "Add Apache 2.0 license"
```

---

### Task 9: Rewrite README for general audience

**Files:**
- Modify: `README.md`

**Step 1: Replace README.md**

The new README should contain these sections in order:

1. **Title and one-liner** — what it does
2. **Disclaimer block** — prominently state: built with Claude Code, provided as-is with no warranty, see LICENSE
3. **How it works** — brief description of the reaction → poll → task flow
4. **Prerequisites** — macOS, OmniFocus, Go 1.22+, Slack user token
5. **Create a Slack app** — step-by-step (reuse existing instructions, they're good)
6. **Build and install** — `go build`, copy to PATH
7. **Usage** — show `poll` and `doctor` commands with flags
8. **Run with launchd** — copy plist, edit token/reaction, load. Include unload.
9. **Multiple workspaces** — explain that each instance gets its own state file automatically; just create additional plist files with different tokens
10. **Troubleshooting** — log location, common errors (updated for flag-based config)
11. **License** — Apache 2.0, link to LICENSE file

**Step 2: Commit**

```bash
git add README.md
git commit -m "Rewrite README for general audience with CLI flag usage"
```

---

### Task 10: Update package doc comments

**Files:**
- Modify: `cmd/slack2omnifocus/main.go` (line 1-3 comment)
- Modify: `internal/slack/client.go` (line 1-3 and ReactedMessage doc)

**Step 1: Update doc comments to not hardcode 👀**

In `cmd/slack2omnifocus/main.go`, update the package comment:
```go
// Command slack2omnifocus turns reacted Slack messages into OmniFocus
// inbox tasks. See docs/plans/ for design documents.
```

In `internal/slack/client.go`, update the `ReactedMessage` doc:
```go
// ReactedMessage is the slack2omnifocus-internal representation of one
// Slack message that the current user has reacted to with the configured
// reaction.
```

**Step 2: Run all tests one final time**

Run: `cd /Users/dan/slack2omnifocus && go test ./... -v`
Expected: All tests PASS.

**Step 3: Commit**

```bash
git add cmd/slack2omnifocus/main.go internal/slack/client.go
git commit -m "Update doc comments to reflect configurable reaction"
```

---

### Task 11: Final verification

**Step 1: Build the binary**

Run: `cd /Users/dan/slack2omnifocus && go build -o slack2omnifocus ./cmd/slack2omnifocus`
Expected: Builds with no errors.

**Step 2: Verify help output**

Run: `./slack2omnifocus help`
Expected: Shows usage with `--token` and `--reaction` flags.

**Step 3: Verify missing flags error**

Run: `./slack2omnifocus poll`
Expected: Error about missing token, exits with code 2.

**Step 4: Run doctor with real token**

The user should run this with their actual token to verify end-to-end.

**Step 5: Push to remote**

```bash
git push
```
