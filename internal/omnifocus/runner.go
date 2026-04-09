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
