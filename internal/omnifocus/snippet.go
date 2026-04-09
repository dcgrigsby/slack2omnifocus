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
