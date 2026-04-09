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
	formatReplace  map[string]string // text → expanded text
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
func (f *fakeSlack) FormatText(ctx context.Context, text string) string {
	if f.formatReplace != nil {
		if out, ok := f.formatReplace[text]; ok {
			return out
		}
	}
	return text
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

func TestRun_expandsSlackEntitiesInTitleAndNote(t *testing.T) {
	// Verifies that FormatText's output is what gets passed to buildTitle
	// and buildNote — i.e. the raw <#C0ARY9XKNTU> style references never
	// reach the OmniFocus task.
	raw := "check out <#C0ARY9XKNTU> and <@U123>"
	expanded := "check out #sharing and @Bob"
	fs := &fakeSlack{
		selfUserID: "USELF",
		items: []SlackMessage{
			{Channel: "C1", Timestamp: "1.0", AuthorUserID: "UAUTHOR", Text: raw},
		},
		displayNames:  map[string]string{"UAUTHOR": "Alice"},
		channelNames:  map[string]string{"C1": "eng-backend"},
		formatReplace: map[string]string{raw: expanded},
	}
	fr := &fakeRunner{}
	store := newStore(t)

	if err := Run(context.Background(), fs, fr, store, alwaysRunningTrue); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(fr.calls))
	}
	payload := fr.calls[0]
	if payload.Name != expanded {
		t.Errorf("task name = %q, want %q (expanded text should be used as title)", payload.Name, expanded)
	}
	if !containsString(payload.Note, expanded) {
		t.Errorf("note should contain expanded text %q:\n%s", expanded, payload.Note)
	}
	if containsString(payload.Note, raw) {
		t.Errorf("note should NOT contain raw entity references:\n%s", payload.Note)
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

func TestRun_reactionRemoveFails_stateStillMarked(t *testing.T) {
	fs := &fakeSlack{
		selfUserID: "USELF",
		items: []SlackMessage{
			{Channel: "C1", Timestamp: "1.0", AuthorUserID: "UAUTHOR", Text: "do the thing"},
		},
		displayNames: map[string]string{"UAUTHOR": "Alice"},
		channelNames: map[string]string{"C1": "eng-backend"},
		removeErr:    map[string]error{"C1:1.0": errors.New("network boom")},
	}
	fr := &fakeRunner{}
	store := newStore(t)

	// Per-message failures are logged, not returned. Run should succeed.
	if err := Run(context.Background(), fs, fr, store, alwaysRunningTrue); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Task WAS created before the (failed) reaction removal.
	if len(fr.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(fr.calls))
	}

	// Reaction removal was attempted exactly once.
	if len(fs.removeCalls) != 1 {
		t.Fatalf("removeCalls = %d, want 1", len(fs.removeCalls))
	}

	// Critical: state MUST be marked even though reaction removal failed.
	// This is what guarantees the next poll retries only the removal and
	// does not re-create the OmniFocus task. If someone ever swaps the
	// order of store.Mark and slack.RemoveEyesReaction in handleMessage,
	// this test will fail.
	if !store.Has("C1", "1.0") {
		t.Error("state should contain C1:1.0 even when reaction removal failed")
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
