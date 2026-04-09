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
