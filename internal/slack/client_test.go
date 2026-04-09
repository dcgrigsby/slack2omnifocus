package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
						{
							// Non-message item type (e.g. a file) — must be filtered out
							// even if the current user reacted with :eyes:.
							"type":    "file",
							"channel": "C3",
							"file": map[string]any{
								"id": "F1",
							},
							"reactions": []map[string]any{
								{"name": "eyes", "count": 1, "users": []string{"USELF"}},
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

func TestChannelName_DMUsesCounterpartDisplayName(t *testing.T) {
	srv := (&fakeSlackServer{
		handlers: map[string]http.HandlerFunc{
			"/conversations.info": func(w http.ResponseWriter, r *http.Request) {
				// DMs have no name and carry an is_im flag plus the
				// counterpart user ID in the "user" field.
				jsonOK(w, map[string]any{
					"channel": map[string]any{
						"id":    "D0ALS43RSKS",
						"is_im": true,
						"user":  "UCOUNTERPART",
						"name":  "",
					},
				})
			},
			"/users.info": func(w http.ResponseWriter, r *http.Request) {
				jsonOK(w, map[string]any{
					"user": map[string]any{
						"id": "UCOUNTERPART",
						"profile": map[string]any{
							"display_name": "DC",
							"real_name":    "D. Counterpart",
						},
					},
				})
			},
		},
	}).start(t)

	c := newForTest(t, srv)
	name, err := c.ChannelName(context.Background(), "D0ALS43RSKS")
	if err != nil {
		t.Fatalf("ChannelName error: %v", err)
	}
	if name != "DC" {
		t.Errorf("name = %q, want %q (DM should resolve to counterpart display name)", name, "DC")
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
	return c
}
