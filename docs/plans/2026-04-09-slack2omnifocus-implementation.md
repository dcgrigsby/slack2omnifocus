# slack2omnifocus Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a Go binary that, when invoked by launchd every 5 minutes, finds Slack messages the user has reacted to with 👀, creates an OmniFocus inbox task for each one via OmniJS, and removes the 👀 reaction as confirmation.

**Architecture:** Single Go binary with `poll` and `doctor` subcommands. No daemon, no webhooks. Reads `SLACK_TOKEN` from the environment (populated from `.env.local` by direnv in shell, or by an `sh -c` wrapper in the launchd plist). Talks to Slack via `github.com/slack-go/slack` and to OmniFocus via `osascript -e 'tell application "OmniFocus" to evaluate javascript "…"'`. Per-message deduplication via a plain-text state file at `~/.local/state/slack2omnifocus/processed.txt`. Mirrors the `.env.local` pattern of the sibling `omnifocal` project.

**Tech Stack:**
- Go 1.22+
- `github.com/slack-go/slack` (only external dependency)
- Standard library only for everything else (`log/slog`, `os/exec`, `encoding/json`, `bufio`, `net/http/httptest` for tests)
- macOS: `osascript`, `pgrep`, `launchd`

**Design reference:** `docs/plans/2026-04-09-slack2omnifocus-design.md` — read this first for the "why."

---

## Coding standards (applies to every task)

- **TDD.** Every non-trivial function gets a failing test written first, run to confirm it fails, then minimal code to make it pass, then run again to confirm.
- **DRY.** If you find yourself duplicating logic, stop and extract.
- **YAGNI.** Do not add features not in this plan. If you think something is missing, ask.
- **Small commits.** Each task ends with a single commit. Commit message format: imperative subject line under ~70 chars, blank line, optional body.
- **Comments explain why, not what.** Assume the reader can read Go.
- **`log/slog` for logging,** text handler, INFO level by default. No `fmt.Println` or `log.Printf` in production code paths.
- **Error wrapping.** Always `fmt.Errorf("context: %w", err)` so call sites can inspect.
- **No globals** except in `main.go`. All dependencies injected.
- **Interfaces defined at the consumer, not the producer.** The `internal/poll` package defines the interfaces it needs for Slack and OmniFocus; `internal/slack` and `internal/omnifocus` just implement concrete types.

---

## Phase 1 — Scaffolding

### Task 1: Initialize Go module and commit scaffolding files

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `.envrc`
- Create: `.env.local.example`
- Create: `README.md` (stub)

**Step 1: Initialize the Go module**

```bash
cd /Users/dan/slack2omnifocus
go mod init github.com/dcgrigsby/slack2omnifocus
```

Expected: creates `go.mod` with `module github.com/dcgrigsby/slack2omnifocus` and a `go 1.22` (or newer) line.

**Step 2: Write `.gitignore`**

Contents (overwrite any existing file):

```
# Build output
/slack2omnifocus

# Secrets — never commit
.env.local
.env

# Launchd log files (written by the daemon, not tracked)
/tmp/slack2omnifocus.*.log

# Editor cruft
.DS_Store
```

**Step 3: Write `.envrc`**

Contents:

```
# Load local secrets (matches the omnifocal project's pattern)
dotenv_if_exists .env.local
```

**Step 4: Write `.env.local.example`**

Contents:

```
# Copy this file to .env.local and fill in the real token.
# .env.local is gitignored and must be chmod 600.
#
# Create the token at https://api.slack.com/apps — see README.md for
# step-by-step setup instructions.

export SLACK_TOKEN=xoxp-REPLACE-ME
```

**Step 5: Write `README.md` stub**

Minimal stub that gets fleshed out in Task 13. Contents:

```markdown
# slack2omnifocus

A personal Mac tool that turns Slack messages into OmniFocus inbox tasks
when you react with 👀.

See `docs/plans/2026-04-09-slack2omnifocus-design.md` for the design.
Full setup instructions land in this README in Task 13.
```

**Step 6: Verify direnv does not already know about `.env.local`**

```bash
ls -la .env.local 2>/dev/null || echo "no .env.local yet — expected"
```

Expected: `no .env.local yet — expected`

**Step 7: Commit**

```bash
git add go.mod .gitignore .envrc .env.local.example README.md
git commit -m "Scaffold Go module and .env.local pattern

Matches the sibling omnifocal project's direnv + .env.local approach
for secrets. The real .env.local is gitignored; .env.local.example is
the template committed to the repo."
```

---

## Phase 2 — Pure packages (unit-testable, no external deps beyond stdlib)

### Task 2: Config package — load `SLACK_TOKEN` from environment

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Step 1: Write the failing test**

File: `internal/config/config_test.go`

```go
package config

import (
	"strings"
	"testing"
)

func TestLoad_happyPath(t *testing.T) {
	t.Setenv("SLACK_TOKEN", "xoxp-test-token-123")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.SlackToken != "xoxp-test-token-123" {
		t.Errorf("SlackToken = %q, want %q", cfg.SlackToken, "xoxp-test-token-123")
	}
}

func TestLoad_missingToken(t *testing.T) {
	t.Setenv("SLACK_TOKEN", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with empty SLACK_TOKEN returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "SLACK_TOKEN") {
		t.Errorf("error message does not mention SLACK_TOKEN: %v", err)
	}
}

func TestLoad_wrongTokenPrefix(t *testing.T) {
	t.Setenv("SLACK_TOKEN", "xoxb-bot-token-should-be-rejected")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with xoxb- token returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "xoxp-") {
		t.Errorf("error message does not mention xoxp-: %v", err)
	}
}

func TestLoad_trimsWhitespace(t *testing.T) {
	t.Setenv("SLACK_TOKEN", "  xoxp-padded-token  \n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.SlackToken != "xoxp-padded-token" {
		t.Errorf("SlackToken = %q, want trimmed %q", cfg.SlackToken, "xoxp-padded-token")
	}
}
```

**Step 2: Run the test and verify it fails**

```bash
go test ./internal/config/...
```

Expected: compilation error (`undefined: Load`) or test failures.

**Step 3: Write the implementation**

File: `internal/config/config.go`

```go
// Package config loads slack2omnifocus runtime configuration from the
// process environment. The .env.local file pattern is intentionally handled
// outside this package (by direnv in a shell or by the launchd wrapper).
// All this package sees is os.Getenv.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Config holds validated runtime configuration.
type Config struct {
	// SlackToken is a Slack user OAuth token (xoxp-...).
	SlackToken string
}

// Load reads configuration from the environment and validates it.
// It returns an error if SLACK_TOKEN is missing or does not look like a
// user token.
func Load() (Config, error) {
	token := strings.TrimSpace(os.Getenv("SLACK_TOKEN"))
	if token == "" {
		return Config{}, errors.New("SLACK_TOKEN environment variable is not set (did you create .env.local and load it via direnv?)")
	}
	if !strings.HasPrefix(token, "xoxp-") {
		return Config{}, fmt.Errorf("SLACK_TOKEN does not look like a user token: expected xoxp- prefix, got %q…", safePrefix(token))
	}
	return Config{SlackToken: token}, nil
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

**Step 4: Run the tests and verify they pass**

```bash
go test ./internal/config/... -v
```

Expected: all four tests pass.

**Step 5: Commit**

```bash
git add internal/config/
git commit -m "Add config package: load SLACK_TOKEN from env

Validates presence and xoxp- prefix. The .env.local file itself is
loaded by direnv in shell or by the launchd wrapper — config.Load
only reads os.Getenv."
```

---

### Task 3: AppleScript string quoting helper

**Files:**
- Create: `internal/omnifocus/applescript.go`
- Create: `internal/omnifocus/applescript_test.go`

**Step 1: Write the failing test**

File: `internal/omnifocus/applescript_test.go`

```go
package omnifocus

import "testing"

func TestApplescriptQuoteString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty",
			input: "",
			want:  `""`,
		},
		{
			name:  "plain ascii",
			input: "hello world",
			want:  `"hello world"`,
		},
		{
			name:  "embedded double quote",
			input: `she said "hi"`,
			want:  `"she said \"hi\""`,
		},
		{
			name:  "embedded backslash",
			input: `path\to\thing`,
			want:  `"path\\to\\thing"`,
		},
		{
			name:  "backslash then quote",
			input: `\"`,
			want:  `"\\\""`,
		},
		{
			name:  "unicode passes through unchanged",
			input: "look 👀 here",
			want:  `"look 👀 here"`,
		},
		{
			name:  "newlines pass through unchanged",
			input: "line one\nline two",
			want:  "\"line one\nline two\"",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := applescriptQuoteString(tc.input)
			if got != tc.want {
				t.Errorf("applescriptQuoteString(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
```

**Step 2: Run the test and verify it fails**

```bash
go test ./internal/omnifocus/...
```

Expected: compilation error (`undefined: applescriptQuoteString`).

**Step 3: Write the implementation**

File: `internal/omnifocus/applescript.go`

```go
package omnifocus

import "strings"

// applescriptQuoteString takes an arbitrary Go string and returns a valid
// AppleScript string literal. AppleScript string literals only require two
// characters to be escaped: backslash (\ → \\) and double quote (" → \").
// Newlines and Unicode pass through unchanged.
//
// Note the escaping order matters: backslashes must be doubled FIRST, then
// double quotes escaped. Doing it the other way around would re-escape the
// backslash that was just added in front of a quote.
func applescriptQuoteString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
```

**Step 4: Run the tests and verify they pass**

```bash
go test ./internal/omnifocus/... -v
```

Expected: all seven sub-tests pass.

**Step 5: Commit**

```bash
git add internal/omnifocus/applescript.go internal/omnifocus/applescript_test.go
git commit -m "Add applescriptQuoteString helper with unit tests

Table-driven tests cover the tricky cases: backslash-then-quote,
Unicode, newlines. Escaping order (backslashes first, then quotes)
is documented in the function comment."
```

---

### Task 4: OmniJS snippet builder

**Files:**
- Create: `internal/omnifocus/snippet.go`
- Create: `internal/omnifocus/snippet_test.go`

**Step 1: Write the failing test**

File: `internal/omnifocus/snippet_test.go`

```go
package omnifocus

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildSnippet_validJSCanBeUnmarshaledBack(t *testing.T) {
	// The snippet contains a JSON-encoded payload literal. We can extract
	// that JSON and confirm our inputs made it through intact, including
	// quotes, backslashes, and newlines.
	payload := TaskPayload{
		Name: `Read "the thing"`,
		Note: "line one\nline two with a \\ backslash\nand 👀 unicode",
	}

	snippet, err := BuildSnippet(payload)
	if err != nil {
		t.Fatalf("BuildSnippet returned error: %v", err)
	}

	// Extract the JSON object literal between "var p = " and "; var t"
	const prefix = "var p = "
	const suffix = "; var t"
	start := strings.Index(snippet, prefix)
	if start == -1 {
		t.Fatalf("snippet missing %q: %s", prefix, snippet)
	}
	start += len(prefix)
	end := strings.Index(snippet[start:], suffix)
	if end == -1 {
		t.Fatalf("snippet missing %q: %s", suffix, snippet)
	}
	jsonLiteral := snippet[start : start+end]

	var roundTripped TaskPayload
	if err := json.Unmarshal([]byte(jsonLiteral), &roundTripped); err != nil {
		t.Fatalf("extracted JSON literal does not unmarshal: %v\nliteral: %s", err, jsonLiteral)
	}
	if roundTripped.Name != payload.Name {
		t.Errorf("Name round-trip failed: got %q, want %q", roundTripped.Name, payload.Name)
	}
	if roundTripped.Note != payload.Note {
		t.Errorf("Note round-trip failed: got %q, want %q", roundTripped.Note, payload.Note)
	}
}

func TestBuildSnippet_containsRequiredJSElements(t *testing.T) {
	snippet, err := BuildSnippet(TaskPayload{Name: "x", Note: "y"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	required := []string{
		"new Task",
		"inbox.ending",
		"t.note = p.note",
		"t.id.primaryKey",
		"JSON.stringify",
	}
	for _, want := range required {
		if !strings.Contains(snippet, want) {
			t.Errorf("snippet missing %q\nsnippet: %s", want, snippet)
		}
	}
}
```

**Step 2: Run the test and verify it fails**

```bash
go test ./internal/omnifocus/... -run BuildSnippet
```

Expected: compilation error (`undefined: BuildSnippet`, `undefined: TaskPayload`).

**Step 3: Write the implementation**

File: `internal/omnifocus/snippet.go`

```go
package omnifocus

import (
	"encoding/json"
	"fmt"
)

// TaskPayload is the data the OmniJS snippet needs to create one inbox task.
type TaskPayload struct {
	Name string `json:"name"`
	Note string `json:"note"`
}

// BuildSnippet returns an OmniJS snippet that, when evaluated by OmniFocus,
// creates a new inbox task with the given name and note, and returns a JSON
// string containing the new task's primary key.
//
// The payload is embedded as a JSON literal in the generated JavaScript.
// Because JSON is a subset of JS, json.Marshal's output is a valid JS object
// literal, which cleanly handles quotes, backslashes, newlines, and Unicode
// in the message text without any hand-rolled JS escaping.
func BuildSnippet(payload TaskPayload) (string, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal task payload: %w", err)
	}
	return fmt.Sprintf(
		`(function(){var p = %s; var t = new Task(p.name, inbox.ending); t.note = p.note; return JSON.stringify({id: t.id.primaryKey});})()`,
		payloadJSON,
	), nil
}
```

**Step 4: Run the tests and verify they pass**

```bash
go test ./internal/omnifocus/... -v
```

Expected: all tests in the package pass (including the prior applescript tests).

**Step 5: Commit**

```bash
git add internal/omnifocus/snippet.go internal/omnifocus/snippet_test.go
git commit -m "Add OmniJS snippet builder for inbox tasks

Embeds the task name and note as a JSON object literal inside the
JS source, relying on the fact that JSON is a subset of JavaScript
to avoid any hand-rolled escaping. The round-trip test confirms
quotes, backslashes, and newlines survive intact."
```

---

### Task 5: State file package — deduplication store

**Files:**
- Create: `internal/state/state.go`
- Create: `internal/state/state_test.go`

**Step 1: Write the failing test**

File: `internal/state/state_test.go`

```go
package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpen_createsMissingDirAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "sub", "processed.txt")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if store.Has("C123", "1.0") {
		t.Errorf("new store unexpectedly reports Has(C123, 1.0) = true")
	}
}

func TestMark_persistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "processed.txt")

	store1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if err := store1.Mark("C123", "1712345678.000001"); err != nil {
		t.Fatalf("Mark() error = %v", err)
	}
	if !store1.Has("C123", "1712345678.000001") {
		t.Error("Has() right after Mark() returned false")
	}

	// Reopen and verify the entry persisted.
	store2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	if !store2.Has("C123", "1712345678.000001") {
		t.Error("Has() after reopen returned false for marked entry")
	}
	if store2.Has("C123", "9999999999.999999") {
		t.Error("Has() returned true for unmarked entry")
	}
}

func TestMark_isIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "processed.txt")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	for i := 0; i < 5; i++ {
		if err := store.Mark("C123", "1.0"); err != nil {
			t.Fatalf("Mark() iteration %d error = %v", i, err)
		}
	}

	// File should contain exactly one line, not five.
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	wantLines := 1
	gotLines := 0
	for _, b := range contents {
		if b == '\n' {
			gotLines++
		}
	}
	if gotLines != wantLines {
		t.Errorf("file has %d lines after %d Mark() calls, want %d\ncontents: %q",
			gotLines, 5, wantLines, contents)
	}
}

func TestMark_multipleDistinctEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "processed.txt")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	entries := [][2]string{
		{"C1", "1.0"},
		{"C1", "2.0"},
		{"C2", "1.0"},
		{"D3", "1712345678.123456"},
	}
	for _, e := range entries {
		if err := store.Mark(e[0], e[1]); err != nil {
			t.Fatalf("Mark(%q, %q) error = %v", e[0], e[1], err)
		}
	}

	for _, e := range entries {
		if !store.Has(e[0], e[1]) {
			t.Errorf("Has(%q, %q) = false after Mark", e[0], e[1])
		}
	}
}
```

**Step 2: Run the test and verify it fails**

```bash
go test ./internal/state/...
```

Expected: compilation error (`undefined: Open`).

**Step 3: Write the implementation**

File: `internal/state/state.go`

```go
// Package state manages the on-disk deduplication store that tracks which
// Slack messages have already been turned into OmniFocus tasks. The store is
// a plain text file with one "channel:timestamp" entry per line.
package state

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store is an in-memory set of processed (channel, timestamp) pairs backed
// by a plain-text file. It is not safe for concurrent use across goroutines,
// but slack2omnifocus uses a single goroutine so that's fine.
type Store struct {
	path string
	seen map[string]struct{}
}

// Open reads the state file at path (creating the parent directory if
// necessary) and returns a Store. If the file does not exist, an empty
// Store is returned and the file will be created on the first Mark() call.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create state dir %q: %w", filepath.Dir(path), err)
	}

	seen := make(map[string]struct{})
	f, err := os.Open(path)
	switch {
	case err == nil:
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			seen[line] = struct{}{}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read state file %q: %w", path, err)
		}
	case os.IsNotExist(err):
		// New store, nothing to load.
	default:
		return nil, fmt.Errorf("open state file %q: %w", path, err)
	}

	return &Store{path: path, seen: seen}, nil
}

// Has reports whether the given (channel, ts) pair has already been marked.
func (s *Store) Has(channel, ts string) bool {
	_, ok := s.seen[key(channel, ts)]
	return ok
}

// Mark records a (channel, ts) pair as processed. It appends to the state
// file and fsyncs before returning, so if this call returns nil the entry
// is durable. Mark is idempotent: marking an already-marked pair is a no-op
// and returns nil without writing anything.
func (s *Store) Mark(channel, ts string) error {
	k := key(channel, ts)
	if _, ok := s.seen[k]; ok {
		return nil
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open state file for append: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, k); err != nil {
		return fmt.Errorf("append state entry: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync state file: %w", err)
	}
	s.seen[k] = struct{}{}
	return nil
}

func key(channel, ts string) string {
	return channel + ":" + ts
}
```

**Step 4: Run the tests and verify they pass**

```bash
go test ./internal/state/... -v
```

Expected: all four tests pass.

**Step 5: Commit**

```bash
git add internal/state/
git commit -m "Add state package: processed-message dedup store

Plain-text file, one channel:ts per line. Mark() is idempotent, appends
with fsync before returning, and updates the in-memory set. Tests cover
fresh file, persistence across reopen, idempotency, and multiple entries."
```

---

## Phase 3 — System-interacting packages

### Task 6: OmniFocus running check

**Files:**
- Create: `internal/omnifocus/running.go`
- Create: `internal/omnifocus/running_test.go`

**Step 1: Write the failing test**

This one is hard to test hermetically (it depends on whether OmniFocus is actually running on the test host), so the test only checks that the function returns without panicking and returns a boolean. A richer test would require dependency injection of the command runner, which is overkill for a one-line function.

File: `internal/omnifocus/running_test.go`

```go
package omnifocus

import "testing"

func TestIsRunning_returnsBool(t *testing.T) {
	// We can't hermetically assert true or false without controlling the
	// test host, so just verify the call completes and returns without
	// panicking. Any boolean value is acceptable.
	got := IsRunning()
	_ = got
}
```

**Step 2: Run the test and verify it fails**

```bash
go test ./internal/omnifocus/... -run IsRunning
```

Expected: compilation error (`undefined: IsRunning`).

**Step 3: Write the implementation**

File: `internal/omnifocus/running.go`

```go
package omnifocus

import "os/exec"

// IsRunning reports whether OmniFocus is currently running.
//
// It uses `pgrep -x OmniFocus`, which exits 0 if any process matches and
// non-zero otherwise. This check is deliberate: we do NOT use
// `tell application "OmniFocus" to ...` because that auto-launches the app
// as a side effect, and the whole point of this check is to avoid auto-launch.
func IsRunning() bool {
	return exec.Command("/usr/bin/pgrep", "-x", "OmniFocus").Run() == nil
}
```

**Step 4: Run the tests and verify they pass**

```bash
go test ./internal/omnifocus/... -v
```

Expected: all tests pass.

**Step 5: Commit**

```bash
git add internal/omnifocus/running.go internal/omnifocus/running_test.go
git commit -m "Add OmniFocus running check via pgrep

Deliberately avoids \`tell application \"OmniFocus\"\` which would
auto-launch the app; the whole point of this check is to detect
absence without triggering launch."
```

---

### Task 7: Task runner — osascript invocation and Runner interface

**Files:**
- Create: `internal/omnifocus/runner.go`
- Create: `internal/omnifocus/runner_test.go`

The `Runner` is the interface the `poll` package will depend on. The concrete `OsascriptRunner` shells out to real osascript. Because that requires OmniFocus to be running on the test host, the unit tests for `OsascriptRunner` are skipped by default and run only when explicitly opted in with `-tags=integration`. We add a fake `Runner` implementation later in the poll tests.

**Step 1: Write the failing test**

File: `internal/omnifocus/runner_test.go`

```go
package omnifocus

import (
	"testing"
)

// Compile-time check: OsascriptRunner must satisfy the Runner interface.
var _ Runner = OsascriptRunner{}

func TestOsascriptRunner_satisfiesInterface(t *testing.T) {
	// Nothing to do at runtime — the compile-time assertion above is the
	// test. This function exists so `go test` has something to call in
	// this file.
}
```

**Step 2: Run the test and verify it fails**

```bash
go test ./internal/omnifocus/... -run OsascriptRunner
```

Expected: compilation error (`undefined: Runner`, `undefined: OsascriptRunner`).

**Step 3: Write the implementation**

File: `internal/omnifocus/runner.go`

```go
package omnifocus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner creates OmniFocus inbox tasks from TaskPayloads.
//
// This is the dependency poll.Poll takes; tests supply a fake, production
// uses OsascriptRunner.
type Runner interface {
	CreateTask(payload TaskPayload) (taskID string, err error)
}

// OsascriptRunner is a Runner that shells out to /usr/bin/osascript to
// evaluate an OmniJS snippet inside OmniFocus. It does NOT check whether
// OmniFocus is running; callers must use IsRunning() first. Doing so here
// would give the Runner two unrelated responsibilities.
type OsascriptRunner struct{}

// CreateTask builds an OmniJS snippet from the payload, wraps it in an
// AppleScript `tell application "OmniFocus" to evaluate javascript ...`
// command, and runs it via osascript.
//
// The result counts as successful only if all of the following hold:
//   - osascript exits 0
//   - stderr is empty
//   - stdout parses as JSON
//   - the parsed object has a non-empty "id" field
//
// Any deviation returns an error. Strict checks are intentional: we'd
// rather re-try a borderline case next poll than silently lose a message.
func (OsascriptRunner) CreateTask(payload TaskPayload) (string, error) {
	snippet, err := BuildSnippet(payload)
	if err != nil {
		return "", err
	}

	script := fmt.Sprintf(
		`tell application "OmniFocus" to evaluate javascript %s`,
		applescriptQuoteString(snippet),
	)

	cmd := exec.Command("/usr/bin/osascript", "-e", script)
	var stderr strings.Builder
	cmd.Stderr = &stderr

	stdout, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("osascript failed: %w (stderr: %s)",
			err, strings.TrimSpace(stderr.String()))
	}
	if stderr.Len() > 0 {
		return "", fmt.Errorf("osascript wrote to stderr: %s",
			strings.TrimSpace(stderr.String()))
	}

	trimmed := strings.TrimSpace(string(stdout))
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(trimmed), &resp); err != nil {
		return "", fmt.Errorf("parse osascript output as JSON: %w (output: %q)", err, trimmed)
	}
	if resp.ID == "" {
		return "", errors.New("osascript returned JSON without a non-empty id field")
	}
	return resp.ID, nil
}
```

**Step 4: Run the tests and verify they pass**

```bash
go test ./internal/omnifocus/... -v
```

Expected: all tests pass.

**Step 5: Commit**

```bash
git add internal/omnifocus/runner.go internal/omnifocus/runner_test.go
git commit -m "Add Runner interface and OsascriptRunner

OsascriptRunner wraps the OmniJS snippet in AppleScript and shells
to osascript. Success requires exit 0, empty stderr, parseable JSON
stdout, and a non-empty id field. Strict checks on purpose: we'd
rather reprocess a message than lose one."
```

---

### Task 8: Slack client wrapper

**Files:**
- Create: `internal/slack/client.go`
- Create: `internal/slack/client_test.go`

This task introduces the `slack-go/slack` dependency. The wrapper's job is threefold:

1. Expose only the operations `poll` needs, behind an interface `poll` can mock.
2. Cache `users.info` and `conversations.info` results for the lifetime of one poll run to avoid N+1 calls.
3. Hide slack-go's ItemRef/ReactedItem types behind our own simple `ReactedMessage` struct so the poll package never imports slack-go.

Tests use `httptest.Server` plus `slack.OptionAPIURL` to point the library at a fake. No real token needed.

**Step 1: Add the slack-go/slack dependency**

```bash
go get github.com/slack-go/slack@latest
go mod tidy
```

**Step 2: Write the failing test**

File: `internal/slack/client_test.go`

```go
package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSlackServer returns an httptest.Server that routes by URL path to
// handler functions for each Slack method the tests need to stub.
type fakeSlackServer struct {
	handlers map[string]http.HandlerFunc
}

func (f *fakeSlackServer) start(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	for path, h := range f.handlers {
		mux.HandleFunc(path, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	// slack-go expects a URL ending with /
	return srv.URL + "/"
}

func jsonOK(w http.ResponseWriter, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	body["ok"] = true
	_ = json.NewEncoder(w).Encode(body)
}

func TestAuthTest_returnsUserID(t *testing.T) {
	srv := (&fakeSlackServer{
		handlers: map[string]http.HandlerFunc{
			"/auth.test": func(w http.ResponseWriter, r *http.Request) {
				jsonOK(w, map[string]any{
					"user":    "danny",
					"user_id": "U0XYZ123",
					"team":    "acme",
					"team_id": "T0ABC000",
					"url":     "https://acme.slack.com/",
				})
			},
		},
	}).start(t)

	c := newForTest(t, srv)
	userID, err := c.AuthTest(context.Background())
	if err != nil {
		t.Fatalf("AuthTest error: %v", err)
	}
	if userID != "U0XYZ123" {
		t.Errorf("userID = %q, want %q", userID, "U0XYZ123")
	}
}

func TestListEyesReactions_filtersByEmojiAndSelf(t *testing.T) {
	// reactions.list returns three items:
	//   1. message with :eyes: by us (and someone else) — INCLUDED
	//   2. message with :eyes: by someone else only — EXCLUDED
	//   3. message with :thumbsup: by us — EXCLUDED
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
								"user": "UAUTHOR1",
								"text": "message one",
								"reactions": []map[string]any{
									{"name": "eyes", "count": 2, "users": []string{"USELF", "UOTHER"}},
								},
							},
						},
						{
							"type":    "message",
							"channel": "C1",
							"message": map[string]any{
								"type": "message",
								"ts":   "2.0",
								"user": "UAUTHOR2",
								"text": "message two",
								"reactions": []map[string]any{
									{"name": "eyes", "count": 1, "users": []string{"UOTHER"}},
								},
							},
						},
						{
							"type":    "message",
							"channel": "C2",
							"message": map[string]any{
								"type": "message",
								"ts":   "3.0",
								"user": "UAUTHOR3",
								"text": "message three",
								"reactions": []map[string]any{
									{"name": "thumbsup", "count": 1, "users": []string{"USELF"}},
								},
							},
						},
					},
					"response_metadata": map[string]any{"next_cursor": ""},
				})
			},
		},
	}).start(t)

	c := newForTest(t, srv)
	items, err := c.ListEyesReactions(context.Background(), "USELF")
	if err != nil {
		t.Fatalf("ListEyesReactions error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1\nitems: %+v", len(items), items)
	}
	got := items[0]
	if got.Channel != "C1" || got.Timestamp != "1.0" || got.AuthorUserID != "UAUTHOR1" || got.Text != "message one" {
		t.Errorf("wrong item: %+v", got)
	}
}

func TestDisplayName_cachesResults(t *testing.T) {
	var calls int
	srv := (&fakeSlackServer{
		handlers: map[string]http.HandlerFunc{
			"/users.info": func(w http.ResponseWriter, r *http.Request) {
				calls++
				jsonOK(w, map[string]any{
					"user": map[string]any{
						"id":   "U123",
						"name": "alice",
						"profile": map[string]any{
							"display_name": "Alice Example",
							"real_name":    "Alice R. Example",
						},
					},
				})
			},
		},
	}).start(t)

	c := newForTest(t, srv)
	for i := 0; i < 3; i++ {
		name, err := c.DisplayName(context.Background(), "U123")
		if err != nil {
			t.Fatalf("DisplayName iteration %d: %v", i, err)
		}
		if name != "Alice Example" {
			t.Errorf("name = %q, want %q", name, "Alice Example")
		}
	}
	if calls != 1 {
		t.Errorf("users.info called %d times, want 1 (should be cached)", calls)
	}
}

func TestDisplayName_fallsBackToRealNameThenID(t *testing.T) {
	srv := (&fakeSlackServer{
		handlers: map[string]http.HandlerFunc{
			"/users.info": func(w http.ResponseWriter, r *http.Request) {
				// No display_name, only real_name
				jsonOK(w, map[string]any{
					"user": map[string]any{
						"id":   "U999",
						"name": "bob",
						"profile": map[string]any{
							"display_name": "",
							"real_name":    "Bob Real",
						},
					},
				})
			},
		},
	}).start(t)

	c := newForTest(t, srv)
	name, err := c.DisplayName(context.Background(), "U999")
	if err != nil {
		t.Fatalf("DisplayName error: %v", err)
	}
	if name != "Bob Real" {
		t.Errorf("name = %q, want %q", name, "Bob Real")
	}
}

func TestRemoveEyesReaction_sendsCorrectRequest(t *testing.T) {
	var gotName, gotChannel, gotTimestamp string
	srv := (&fakeSlackServer{
		handlers: map[string]http.HandlerFunc{
			"/reactions.remove": func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				gotName = r.FormValue("name")
				gotChannel = r.FormValue("channel")
				gotTimestamp = r.FormValue("timestamp")
				jsonOK(w, map[string]any{})
			},
		},
	}).start(t)

	c := newForTest(t, srv)
	if err := c.RemoveEyesReaction(context.Background(), "C7", "42.17"); err != nil {
		t.Fatalf("RemoveEyesReaction error: %v", err)
	}
	if gotName != "eyes" {
		t.Errorf("name = %q, want %q", gotName, "eyes")
	}
	if gotChannel != "C7" {
		t.Errorf("channel = %q, want %q", gotChannel, "C7")
	}
	if gotTimestamp != "42.17" {
		t.Errorf("timestamp = %q, want %q", gotTimestamp, "42.17")
	}
}

// helper to build a Client pointed at the fake server
func newForTest(t *testing.T, fakeURL string) *Client {
	t.Helper()
	c, err := NewWithURL("xoxp-test-token", fakeURL)
	if err != nil {
		t.Fatalf("NewWithURL error: %v", err)
	}
	// Bypass the xoxp- prefix assertion when tests hit methods; tests pass
	// a valid xoxp- token anyway.
	_ = strings.TrimSpace // touch imports
	return c
}
```

**Step 3: Run the test and verify it fails**

```bash
go test ./internal/slack/...
```

Expected: compilation error — many undefined symbols (`NewWithURL`, `Client`, `AuthTest`, `ListEyesReactions`, etc.).

**Step 4: Write the implementation**

File: `internal/slack/client.go`

```go
// Package slack wraps github.com/slack-go/slack, exposing only the
// operations slack2omnifocus needs and hiding slack-go's types from the
// rest of the codebase.
package slack

import (
	"context"
	"fmt"

	slackgo "github.com/slack-go/slack"
)

// ReactedMessage is the slack2omnifocus-internal representation of one
// Slack message that the current user has reacted to with 👀.
type ReactedMessage struct {
	Channel      string // e.g. "C024BE91L"
	Timestamp    string // e.g. "1712345678.123456"
	AuthorUserID string // user ID of the message's author
	Text         string // raw message text
}

// Client talks to the Slack Web API with a user OAuth token.
type Client struct {
	api *slackgo.Client

	userNameCache    map[string]string
	channelNameCache map[string]string
}

// New returns a Client bound to the real Slack API.
func New(token string) *Client {
	return &Client{
		api:              slackgo.New(token),
		userNameCache:    map[string]string{},
		channelNameCache: map[string]string{},
	}
}

// NewWithURL returns a Client bound to a custom API URL (used by tests).
func NewWithURL(token, apiURL string) (*Client, error) {
	return &Client{
		api:              slackgo.New(token, slackgo.OptionAPIURL(apiURL)),
		userNameCache:    map[string]string{},
		channelNameCache: map[string]string{},
	}, nil
}

// AuthTest calls auth.test and returns the authenticated user's ID.
func (c *Client) AuthTest(ctx context.Context) (string, error) {
	resp, err := c.api.AuthTestContext(ctx)
	if err != nil {
		return "", fmt.Errorf("slack auth.test: %w", err)
	}
	return resp.UserID, nil
}

// ListEyesReactions returns every message in the authenticated user's
// reaction history where THAT user's reactions include 👀 (`:eyes:`).
// It paginates through reactions.list.
func (c *Client) ListEyesReactions(ctx context.Context, selfUserID string) ([]ReactedMessage, error) {
	var out []ReactedMessage
	cursor := ""
	for {
		params := slackgo.ListReactionsParameters{
			User:   selfUserID,
			Cursor: cursor,
			Limit:  100,
			Full:   true,
		}
		items, nextCursor, err := c.api.ListReactionsContext(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("slack reactions.list: %w", err)
		}
		for _, it := range items {
			if it.Type != "message" || it.Message == nil {
				continue
			}
			hasSelfEyes := false
			for _, r := range it.Reactions {
				if r.Name != "eyes" {
					continue
				}
				for _, u := range r.Users {
					if u == selfUserID {
						hasSelfEyes = true
						break
					}
				}
				if hasSelfEyes {
					break
				}
			}
			if !hasSelfEyes {
				continue
			}
			out = append(out, ReactedMessage{
				Channel:      it.Channel,
				Timestamp:    it.Message.Timestamp,
				AuthorUserID: it.Message.User,
				Text:         it.Message.Text,
			})
		}
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return out, nil
}

// DisplayName returns a human-friendly name for a Slack user ID, preferring
// display_name, falling back to real_name, then to the ID itself. Results
// are cached for the lifetime of the Client.
func (c *Client) DisplayName(ctx context.Context, userID string) (string, error) {
	if cached, ok := c.userNameCache[userID]; ok {
		return cached, nil
	}
	user, err := c.api.GetUserInfoContext(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("slack users.info %s: %w", userID, err)
	}
	name := user.Profile.DisplayName
	if name == "" {
		name = user.Profile.RealName
	}
	if name == "" {
		name = userID
	}
	c.userNameCache[userID] = name
	return name, nil
}

// ChannelName returns a human-friendly channel name (without leading #) for
// a Slack channel/IM/MPIM ID. Results cached for Client lifetime.
func (c *Client) ChannelName(ctx context.Context, channelID string) (string, error) {
	if cached, ok := c.channelNameCache[channelID]; ok {
		return cached, nil
	}
	ch, err := c.api.GetConversationInfoContext(ctx, &slackgo.GetConversationInfoInput{
		ChannelID: channelID,
	})
	if err != nil {
		return "", fmt.Errorf("slack conversations.info %s: %w", channelID, err)
	}
	name := ch.Name
	if name == "" {
		name = channelID
	}
	c.channelNameCache[channelID] = name
	return name, nil
}

// Permalink returns the https://… deep link for a specific message.
func (c *Client) Permalink(ctx context.Context, channel, ts string) (string, error) {
	link, err := c.api.GetPermalinkContext(ctx, &slackgo.PermalinkParameters{
		Channel: channel,
		Ts:      ts,
	})
	if err != nil {
		return "", fmt.Errorf("slack chat.getPermalink: %w", err)
	}
	return link, nil
}

// RemoveEyesReaction removes the authenticated user's 👀 reaction from
// the given message.
func (c *Client) RemoveEyesReaction(ctx context.Context, channel, ts string) error {
	err := c.api.RemoveReactionContext(ctx, "eyes", slackgo.NewRefToMessage(channel, ts))
	if err != nil {
		return fmt.Errorf("slack reactions.remove: %w", err)
	}
	return nil
}
```

**Step 5: Run the tests and verify they pass**

```bash
go test ./internal/slack/... -v
```

Expected: all five tests pass.

**Step 6: Commit**

```bash
git add go.mod go.sum internal/slack/
git commit -m "Add Slack client wrapper with httptest-based unit tests

Wraps slack-go with a narrow interface: AuthTest, ListEyesReactions,
DisplayName, ChannelName, Permalink, RemoveEyesReaction. In-memory
caches for users.info and conversations.info avoid N+1 calls within
a single poll run. Tests use httptest.Server + slack.OptionAPIURL
so no real token is needed."
```

---

## Phase 4 — Integration

### Task 9: Poll orchestration

**Files:**
- Create: `internal/poll/poll.go`
- Create: `internal/poll/poll_test.go`

The `poll` package owns the main flow. It takes three dependencies: a `SlackClient` interface, a `TaskRunner` interface (which is `omnifocus.Runner`), and a `state.Store`. The interfaces are defined inside the poll package, not re-exported from `internal/slack` — this keeps the consumer in charge of its dependencies.

**Step 1: Write the failing test**

File: `internal/poll/poll_test.go`

```go
package poll

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/dcgrigsby/slack2omnifocus/internal/omnifocus"
	"github.com/dcgrigsby/slack2omnifocus/internal/state"
)

// --- fakes ---

type fakeSlack struct {
	selfUserID string

	items          []SlackMessage
	listErr        error
	removeCalls    []ref
	removeErr      map[string]error // keyed by "channel:ts"
	displayNames   map[string]string
	channelNames   map[string]string
	permalinks     map[string]string // keyed by "channel:ts"
}

type ref struct {
	channel string
	ts      string
}

func (f *fakeSlack) AuthTest(ctx context.Context) (string, error) {
	return f.selfUserID, nil
}
func (f *fakeSlack) ListEyesReactions(ctx context.Context, selfUserID string) ([]SlackMessage, error) {
	return f.items, f.listErr
}
func (f *fakeSlack) DisplayName(ctx context.Context, userID string) (string, error) {
	if n, ok := f.displayNames[userID]; ok {
		return n, nil
	}
	return userID, nil
}
func (f *fakeSlack) ChannelName(ctx context.Context, channelID string) (string, error) {
	if n, ok := f.channelNames[channelID]; ok {
		return n, nil
	}
	return channelID, nil
}
func (f *fakeSlack) Permalink(ctx context.Context, channel, ts string) (string, error) {
	key := channel + ":" + ts
	if p, ok := f.permalinks[key]; ok {
		return p, nil
	}
	return "https://example.slack.com/archives/" + channel + "/p" + ts, nil
}
func (f *fakeSlack) RemoveEyesReaction(ctx context.Context, channel, ts string) error {
	f.removeCalls = append(f.removeCalls, ref{channel, ts})
	if f.removeErr != nil {
		if err, ok := f.removeErr[channel+":"+ts]; ok {
			return err
		}
	}
	return nil
}

type fakeRunner struct {
	calls []omnifocus.TaskPayload
	err   error
}

func (f *fakeRunner) CreateTask(p omnifocus.TaskPayload) (string, error) {
	f.calls = append(f.calls, p)
	if f.err != nil {
		return "", f.err
	}
	return "fake-id-" + p.Name, nil
}

// --- tests ---

func newStore(t *testing.T) *state.Store {
	t.Helper()
	s, err := state.Open(filepath.Join(t.TempDir(), "processed.txt"))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	return s
}

func TestRun_createsTaskAndRemovesReaction_happyPath(t *testing.T) {
	fs := &fakeSlack{
		selfUserID: "USELF",
		items: []SlackMessage{
			{Channel: "C1", Timestamp: "1.0", AuthorUserID: "UAUTHOR", Text: "do the thing"},
		},
		displayNames: map[string]string{"UAUTHOR": "Alice"},
		channelNames: map[string]string{"C1": "eng-backend"},
	}
	fr := &fakeRunner{}
	store := newStore(t)

	if err := Run(context.Background(), fs, fr, store, alwaysRunningTrue); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(fr.calls))
	}
	payload := fr.calls[0]
	if payload.Name != "do the thing" {
		t.Errorf("task name = %q, want %q", payload.Name, "do the thing")
	}
	wantNoteSubstrings := []string{
		"From @Alice in #eng-backend",
		"https://example.slack.com/archives/C1/p1.0",
		"do the thing",
	}
	for _, s := range wantNoteSubstrings {
		if !containsString(payload.Note, s) {
			t.Errorf("note missing %q:\n%s", s, payload.Note)
		}
	}
	if len(fs.removeCalls) != 1 || fs.removeCalls[0] != (ref{"C1", "1.0"}) {
		t.Errorf("removeCalls = %+v, want [{C1 1.0}]", fs.removeCalls)
	}
	if !store.Has("C1", "1.0") {
		t.Error("store should contain C1:1.0 after successful run")
	}
}

func TestRun_omniFocusNotRunning_isANoOp(t *testing.T) {
	fs := &fakeSlack{selfUserID: "USELF", items: []SlackMessage{
		{Channel: "C1", Timestamp: "1.0", Text: "x"},
	}}
	fr := &fakeRunner{}
	store := newStore(t)

	if err := Run(context.Background(), fs, fr, store, alwaysRunningFalse); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 0 {
		t.Errorf("runner called %d times, want 0", len(fr.calls))
	}
	if len(fs.removeCalls) != 0 {
		t.Errorf("removeCalls = %+v, want empty", fs.removeCalls)
	}
	if store.Has("C1", "1.0") {
		t.Error("store should NOT contain C1:1.0 when OmniFocus is not running")
	}
}

func TestRun_alreadyProcessed_skipsTaskCreationButRetriesRemoval(t *testing.T) {
	store := newStore(t)
	if err := store.Mark("C1", "1.0"); err != nil {
		t.Fatalf("seeding store: %v", err)
	}
	fs := &fakeSlack{
		selfUserID: "USELF",
		items: []SlackMessage{
			{Channel: "C1", Timestamp: "1.0", Text: "already processed"},
		},
	}
	fr := &fakeRunner{}

	if err := Run(context.Background(), fs, fr, store, alwaysRunningTrue); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 0 {
		t.Errorf("runner called %d times, want 0 (already processed)", len(fr.calls))
	}
	if len(fs.removeCalls) != 1 {
		t.Errorf("removeCalls = %d, want 1 (retry removal)", len(fs.removeCalls))
	}
}

func TestRun_runnerFails_leavesReactionAndState(t *testing.T) {
	fs := &fakeSlack{
		selfUserID: "USELF",
		items: []SlackMessage{
			{Channel: "C1", Timestamp: "1.0", Text: "x"},
		},
	}
	fr := &fakeRunner{err: errors.New("osascript boom")}
	store := newStore(t)

	// Run should not return an error; per-message failures are logged.
	if err := Run(context.Background(), fs, fr, store, alwaysRunningTrue); err != nil {
		t.Fatalf("Run should not return error on per-message failure, got %v", err)
	}
	if len(fs.removeCalls) != 0 {
		t.Errorf("removeCalls = %+v, want empty on runner failure", fs.removeCalls)
	}
	if store.Has("C1", "1.0") {
		t.Error("store should NOT contain C1:1.0 on runner failure")
	}
}

func TestRun_titleTruncation(t *testing.T) {
	longText := "word word word word word word word word word word word word word word word word word word word"
	fs := &fakeSlack{
		selfUserID: "USELF",
		items: []SlackMessage{
			{Channel: "C1", Timestamp: "1.0", Text: longText},
		},
	}
	fr := &fakeRunner{}
	store := newStore(t)

	if err := Run(context.Background(), fs, fr, store, alwaysRunningTrue); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(fr.calls))
	}
	name := fr.calls[0].Name
	// Title should end with … and be no more than 80 runes (including the …).
	if !endsWithEllipsis(name) {
		t.Errorf("truncated name should end with …: %q", name)
	}
	if runeCount(name) > 80 {
		t.Errorf("truncated name has %d runes, want <= 80: %q", runeCount(name), name)
	}
	// Full text should still be in the note.
	if !containsString(fr.calls[0].Note, longText) {
		t.Errorf("note should contain full long text")
	}
}

func TestRun_newlineInTitleIsFlattened(t *testing.T) {
	fs := &fakeSlack{
		selfUserID: "USELF",
		items: []SlackMessage{
			{Channel: "C1", Timestamp: "1.0", Text: "line one\nline two"},
		},
	}
	fr := &fakeRunner{}
	store := newStore(t)

	if err := Run(context.Background(), fs, fr, store, alwaysRunningTrue); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if containsRune(fr.calls[0].Name, '\n') {
		t.Errorf("task name should not contain newline: %q", fr.calls[0].Name)
	}
	// But the note should retain the newline.
	if !containsString(fr.calls[0].Note, "line one\nline two") {
		t.Errorf("note should retain original newlines")
	}
}

// --- tiny helpers to avoid importing strings/unicode in every test ---

func alwaysRunningTrue() bool  { return true }
func alwaysRunningFalse() bool { return false }

func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func containsRune(s string, r rune) bool {
	for _, x := range s {
		if x == r {
			return true
		}
	}
	return false
}

func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func endsWithEllipsis(s string) bool {
	if s == "" {
		return false
	}
	runes := []rune(s)
	return runes[len(runes)-1] == '…'
}
```

**Step 2: Run the test and verify it fails**

```bash
go test ./internal/poll/...
```

Expected: many compilation errors (`undefined: Run`, `undefined: SlackMessage`, etc.).

**Step 3: Write the implementation**

File: `internal/poll/poll.go`

```go
// Package poll orchestrates one slack2omnifocus poll cycle: list 👀-reacted
// Slack messages, create matching OmniFocus inbox tasks, and remove the 👀
// reactions from successfully-processed messages.
package poll

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dcgrigsby/slack2omnifocus/internal/omnifocus"
)

// SlackMessage mirrors internal/slack.ReactedMessage but is redefined in
// this package to keep the poll package from depending on the slack
// package's concrete types. The poll package defines the interfaces it
// consumes; the slack package implements them.
type SlackMessage struct {
	Channel      string
	Timestamp    string
	AuthorUserID string
	Text         string
}

// SlackClient is the slack-facing dependency of Run.
type SlackClient interface {
	AuthTest(ctx context.Context) (string, error)
	ListEyesReactions(ctx context.Context, selfUserID string) ([]SlackMessage, error)
	DisplayName(ctx context.Context, userID string) (string, error)
	ChannelName(ctx context.Context, channelID string) (string, error)
	Permalink(ctx context.Context, channel, ts string) (string, error)
	RemoveEyesReaction(ctx context.Context, channel, ts string) error
}

// Store is the deduplication-store dependency of Run.
type Store interface {
	Has(channel, ts string) bool
	Mark(channel, ts string) error
}

// IsRunningFunc reports whether OmniFocus is currently running.
type IsRunningFunc func() bool

// Run performs one poll cycle. Per-message failures are logged but do not
// fail the whole run. Run only returns an error for setup-level failures
// (auth.test, listing reactions) because those mean nothing can proceed.
func Run(
	ctx context.Context,
	slack SlackClient,
	runner omnifocus.Runner,
	store Store,
	isRunning IsRunningFunc,
) error {
	if !isRunning() {
		slog.Info("OmniFocus not running; skipping poll")
		return nil
	}

	selfUserID, err := slack.AuthTest(ctx)
	if err != nil {
		return fmt.Errorf("auth.test: %w", err)
	}

	items, err := slack.ListEyesReactions(ctx, selfUserID)
	if err != nil {
		return fmt.Errorf("list eyes reactions: %w", err)
	}

	slog.Info("poll cycle starting",
		slog.String("self_user_id", selfUserID),
		slog.Int("candidates", len(items)),
	)

	for _, msg := range items {
		if err := handleMessage(ctx, slack, runner, store, msg); err != nil {
			slog.Error("processing message failed; leaving reaction in place",
				slog.String("channel", msg.Channel),
				slog.String("ts", msg.Timestamp),
				slog.Any("error", err),
			)
		}
	}

	return nil
}

// handleMessage processes a single candidate. It returns an error to allow
// the caller to log it, but the error is never propagated upward: one bad
// message must not abort the whole poll.
func handleMessage(
	ctx context.Context,
	slack SlackClient,
	runner omnifocus.Runner,
	store Store,
	msg SlackMessage,
) error {
	if store.Has(msg.Channel, msg.Timestamp) {
		// Already turned into a task on a previous run. The reaction may
		// still be present because reactions.remove failed last time; try
		// to remove it again, but do NOT recreate the task.
		slog.Info("already processed; retrying reaction removal",
			slog.String("channel", msg.Channel),
			slog.String("ts", msg.Timestamp),
		)
		return slack.RemoveEyesReaction(ctx, msg.Channel, msg.Timestamp)
	}

	// Enrich with author name, channel name, and permalink.
	authorName, err := slack.DisplayName(ctx, msg.AuthorUserID)
	if err != nil {
		return fmt.Errorf("resolve author name: %w", err)
	}
	channelName, err := slack.ChannelName(ctx, msg.Channel)
	if err != nil {
		return fmt.Errorf("resolve channel name: %w", err)
	}
	permalink, err := slack.Permalink(ctx, msg.Channel, msg.Timestamp)
	if err != nil {
		return fmt.Errorf("resolve permalink: %w", err)
	}

	payload := omnifocus.TaskPayload{
		Name: buildTitle(msg.Text),
		Note: buildNote(authorName, channelName, permalink, msg.Text),
	}

	taskID, err := runner.CreateTask(payload)
	if err != nil {
		return fmt.Errorf("create OmniFocus task: %w", err)
	}
	slog.Info("created OmniFocus task",
		slog.String("task_id", taskID),
		slog.String("channel", msg.Channel),
		slog.String("ts", msg.Timestamp),
	)

	// Persist dedup state BEFORE removing the reaction. If removal fails,
	// the next poll will see the entry in the state file and just retry
	// removal instead of re-creating the task.
	if err := store.Mark(msg.Channel, msg.Timestamp); err != nil {
		return fmt.Errorf("mark state: %w", err)
	}

	if err := slack.RemoveEyesReaction(ctx, msg.Channel, msg.Timestamp); err != nil {
		// State is already marked; next poll will retry removal.
		return fmt.Errorf("remove eyes reaction: %w", err)
	}
	return nil
}

// buildTitle produces a one-line, truncated task title from a message body.
const titleMaxRunes = 80

func buildTitle(text string) string {
	// Flatten newlines to spaces so the title stays single-line.
	flat := strings.Join(strings.Fields(text), " ")
	runes := []rune(flat)
	if len(runes) <= titleMaxRunes {
		return flat
	}
	// Reserve one rune for the ellipsis.
	return string(runes[:titleMaxRunes-1]) + "…"
}

func buildNote(authorName, channelName, permalink, text string) string {
	return fmt.Sprintf(
		"From @%s in #%s\n%s\n\n%s",
		authorName, channelName, permalink, text,
	)
}
```

**Step 4: Run the tests and verify they pass**

```bash
go test ./internal/poll/... -v
```

Expected: all six tests pass.

**Step 5: Commit**

```bash
git add internal/poll/
git commit -m "Add poll orchestration with full unit test coverage

Run() handles the four interesting paths: happy path, OmniFocus not
running (no-op), already processed (retry removal only), runner failure
(leave reaction in place). buildTitle flattens whitespace and truncates
at 80 runes with an ellipsis; buildNote assembles the metadata header."
```

---

## Phase 5 — Gluing the pieces together

### Task 10: Wire `internal/slack.Client` into `poll.SlackClient`

`internal/slack.Client` returns `ReactedMessage`, but `poll.SlackClient` wants `SlackMessage`. These are structurally identical — one tiny adapter handles the conversion.

**Files:**
- Create: `internal/slack/poll_adapter.go`
- Create: `internal/slack/poll_adapter_test.go`

**Step 1: Write the failing test**

File: `internal/slack/poll_adapter_test.go`

```go
package slack

import (
	"testing"

	"github.com/dcgrigsby/slack2omnifocus/internal/poll"
)

// Compile-time assertion: PollAdapter must satisfy poll.SlackClient.
var _ poll.SlackClient = (*PollAdapter)(nil)

func TestPollAdapter_exists(t *testing.T) {}
```

**Step 2: Run the test and verify it fails**

```bash
go test ./internal/slack/... -run PollAdapter
```

Expected: compilation error (`undefined: PollAdapter`).

**Step 3: Write the implementation**

File: `internal/slack/poll_adapter.go`

```go
package slack

import (
	"context"

	"github.com/dcgrigsby/slack2omnifocus/internal/poll"
)

// PollAdapter wraps a *Client so it satisfies poll.SlackClient, converting
// ReactedMessage to poll.SlackMessage on the way out.
type PollAdapter struct {
	Client *Client
}

func (a *PollAdapter) AuthTest(ctx context.Context) (string, error) {
	return a.Client.AuthTest(ctx)
}

func (a *PollAdapter) ListEyesReactions(ctx context.Context, selfUserID string) ([]poll.SlackMessage, error) {
	items, err := a.Client.ListEyesReactions(ctx, selfUserID)
	if err != nil {
		return nil, err
	}
	out := make([]poll.SlackMessage, len(items))
	for i, it := range items {
		out[i] = poll.SlackMessage{
			Channel:      it.Channel,
			Timestamp:    it.Timestamp,
			AuthorUserID: it.AuthorUserID,
			Text:         it.Text,
		}
	}
	return out, nil
}

func (a *PollAdapter) DisplayName(ctx context.Context, userID string) (string, error) {
	return a.Client.DisplayName(ctx, userID)
}

func (a *PollAdapter) ChannelName(ctx context.Context, channelID string) (string, error) {
	return a.Client.ChannelName(ctx, channelID)
}

func (a *PollAdapter) Permalink(ctx context.Context, channel, ts string) (string, error) {
	return a.Client.Permalink(ctx, channel, ts)
}

func (a *PollAdapter) RemoveEyesReaction(ctx context.Context, channel, ts string) error {
	return a.Client.RemoveEyesReaction(ctx, channel, ts)
}
```

**Step 4: Run the tests and verify they pass**

```bash
go test ./internal/slack/... -v
go test ./internal/poll/... -v
```

Expected: all tests in both packages pass.

**Step 5: Commit**

```bash
git add internal/slack/poll_adapter.go internal/slack/poll_adapter_test.go
git commit -m "Add PollAdapter wiring internal/slack.Client to poll.SlackClient"
```

---

### Task 11: CLI entry point (`cmd/slack2omnifocus/main.go`)

**Files:**
- Create: `cmd/slack2omnifocus/main.go`

No new tests — `main.go` is thin plumbing that each piece below has covered separately. We verify by running `go build` and `slack2omnifocus --help`.

**Step 1: Write `main.go`**

File: `cmd/slack2omnifocus/main.go`

```go
// Command slack2omnifocus turns 👀-reacted Slack messages into OmniFocus
// inbox tasks. See docs/plans/2026-04-09-slack2omnifocus-design.md for
// the full design.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

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
		if err := runPoll(); err != nil {
			slog.Error("poll failed", slog.Any("error", err))
			os.Exit(1)
		}
	case "doctor":
		if err := runDoctor(); err != nil {
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

func usage() {
	fmt.Fprint(os.Stderr, `slack2omnifocus — turn 👀-reacted Slack messages into OmniFocus inbox tasks

Usage:
  slack2omnifocus poll     run one poll cycle and exit
  slack2omnifocus doctor   sanity-check setup (token, OmniFocus, state file)
  slack2omnifocus help     show this message

Configuration is read from environment variables; create .env.local in
the repo root with `+"`export SLACK_TOKEN=xoxp-...`"+` and let direnv load it,
or use the sh -c wrapper in the provided launchd plist.
`)
}

func runPoll() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	slackClient := slack.New(cfg.SlackToken)
	adapter := &slack.PollAdapter{Client: slackClient}
	runner := omnifocus.OsascriptRunner{}

	statePath, err := defaultStatePath()
	if err != nil {
		return err
	}
	store, err := state.Open(statePath)
	if err != nil {
		return err
	}

	return poll.Run(context.Background(), adapter, runner, store, omnifocus.IsRunning)
}

func runDoctor() error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ config:", err)
		return err
	}
	fmt.Println("✓ SLACK_TOKEN loaded (prefix OK)")

	client := slack.New(cfg.SlackToken)
	userID, err := client.AuthTest(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ slack auth.test:", err)
		return err
	}
	fmt.Printf("✓ Slack auth.test OK (user_id=%s)\n", userID)

	if omnifocus.IsRunning() {
		fmt.Println("✓ OmniFocus is running")
	} else {
		fmt.Println("⚠ OmniFocus is NOT running — poll will skip until it is")
	}

	statePath, err := defaultStatePath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ state path:", err)
		return err
	}
	if _, err := state.Open(statePath); err != nil {
		fmt.Fprintln(os.Stderr, "✗ state file:", err)
		return err
	}
	fmt.Printf("✓ State file writable at %s\n", statePath)
	return nil
}

func defaultStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "slack2omnifocus", "processed.txt"), nil
}
```

**Step 2: Build the binary**

```bash
go build -o slack2omnifocus ./cmd/slack2omnifocus
```

Expected: compiles cleanly, produces `./slack2omnifocus` in the repo root.

**Step 3: Run `--help` to sanity-check the CLI**

```bash
./slack2omnifocus --help
```

Expected: usage text is printed.

**Step 4: Run the full test suite**

```bash
go test ./...
```

Expected: all tests pass.

**Step 5: Commit**

```bash
git add cmd/slack2omnifocus/main.go
git commit -m "Add slack2omnifocus CLI with poll and doctor subcommands

main.go is thin plumbing: config.Load → slack.New → omnifocus runner
→ state.Open → poll.Run. Doctor subcommand checks token validity,
OmniFocus running state, and state file writability."
```

---

## Phase 6 — Deployment artifacts

### Task 12: launchd plist

**Files:**
- Create: `launchd/com.slack2omnifocus.poll.plist`

**Step 1: Write the plist**

File: `launchd/com.slack2omnifocus.poll.plist`

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

**Step 2: Validate the plist syntactically**

```bash
plutil -lint launchd/com.slack2omnifocus.poll.plist
```

Expected: `launchd/com.slack2omnifocus.poll.plist: OK`

**Step 3: Commit**

```bash
git add launchd/com.slack2omnifocus.poll.plist
git commit -m "Add launchd plist: sh -c wrapper sources .env.local before exec

Runs slack2omnifocus poll every 300s, RunAtLoad so first fire happens
on launchctl load, logs to /tmp/slack2omnifocus.*.log matching the
omnifocal project's convention."
```

---

### Task 13: Full README

**Files:**
- Modify: `README.md`

Replace the stub from Task 1 with the full setup guide. Include: what it does, prerequisites, Slack app setup (copy from design doc), `.env.local` setup, build/install, launchd install, doctor, troubleshooting.

**Step 1: Write the full README**

File: `README.md`

Use the following as the starting structure (fill in prose as you write; this is a skeleton):

```markdown
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
```

**Step 2: Commit**

```bash
git add README.md
git commit -m "Flesh out README with full setup and troubleshooting"
```

---

## Phase 7 — Final verification

### Task 14: End-to-end manual verification

This task does not produce code; it runs the tool against the real Slack API and real OmniFocus to confirm the pieces fit together. Only run this task *after* all prior tasks pass in isolation.

**Step 1: Confirm the whole test suite passes**

```bash
go test ./...
```

Expected: PASS for every package.

**Step 2: Build a fresh binary**

```bash
go build -o slack2omnifocus ./cmd/slack2omnifocus
```

**Step 3: Run doctor and sanity-check output**

```bash
./slack2omnifocus doctor
```

Expected output includes:

```
✓ SLACK_TOKEN loaded (prefix OK)
✓ Slack auth.test OK (user_id=U...)
✓ OmniFocus is running
✓ State file writable at /Users/dan/.local/state/slack2omnifocus/processed.txt
```

If any check fails, stop and investigate — do not proceed until all four are green.

**Step 4: React to a test message in Slack**

In any channel or DM, post a throwaway message and react to it with 👀. Note the channel and approximate timestamp — you'll verify this specific message below.

**Step 5: Run one poll**

```bash
./slack2omnifocus poll
```

Expected in stderr: `poll cycle starting`, `created OmniFocus task`, and no ERROR lines.

**Step 6: Verify in OmniFocus**

Open OmniFocus, look in the inbox. You should see a new task whose name is the first part of your test message (truncated if it was long), and whose note contains `From @<your name> in #<channel>`, a `https://…slack.com/archives/…` link, and the full message text.

**Step 7: Verify in Slack**

Return to Slack. Your 👀 reaction on the test message should now be gone.

**Step 8: Verify the state file**

```bash
cat ~/.local/state/slack2omnifocus/processed.txt
```

Expected: at least one line in the form `<channel_id>:<timestamp>` corresponding to your test message.

**Step 9: Install the launchd job**

```bash
sudo cp ./slack2omnifocus /usr/local/bin/slack2omnifocus
cp launchd/com.slack2omnifocus.poll.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.slack2omnifocus.poll.plist
```

`RunAtLoad` fires the first run immediately. Watch `/tmp/slack2omnifocus.stderr.log` to see the log output.

**Step 10: End-to-end launchd verification**

React to another test message with 👀. Within 5 minutes (most likely within a few seconds if you catch a scheduled interval, otherwise up to 5 minutes), the message should appear in OmniFocus and the 👀 should be removed.

Check `/tmp/slack2omnifocus.stderr.log` and `/tmp/slack2omnifocus.stdout.log` for the log lines corresponding to that poll cycle.

**Step 11: No commit needed**

This task is pure verification; no new files are produced.

---

## Done criteria

slack2omnifocus is "done" when all of the following are true:

- `go test ./...` passes with no failures or skipped tests.
- `go build -o slack2omnifocus ./cmd/slack2omnifocus` produces a binary with no warnings.
- `plutil -lint launchd/com.slack2omnifocus.poll.plist` returns OK.
- `slack2omnifocus doctor` prints all four green checks against a real Slack token and a running OmniFocus.
- An end-to-end test (react with 👀, wait for a poll, confirm OmniFocus task and reaction removal) succeeds twice in a row — once via manual `slack2omnifocus poll` invocation and once via the installed launchd job.
- A second manual `poll` run immediately after the first one produces no new tasks and no errors (dedup works).
- Reacting to a message while OmniFocus is quit, then running `poll`, leaves the reaction in place and produces no task; launching OmniFocus and running `poll` again then processes the message normally.
