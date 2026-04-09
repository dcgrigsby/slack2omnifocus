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

func TestFormatText_plainTextUnchanged(t *testing.T) {
	c := newForTest(t, (&fakeSlackServer{}).start(t))
	got := c.FormatText(context.Background(), "just some plain text with no entities")
	if got != "just some plain text with no entities" {
		t.Errorf("FormatText changed plain text: %q", got)
	}
}

func TestFormatText_channelMentionWithInlineName(t *testing.T) {
	// When Slack includes the |name after the ID, no API call is needed.
	c := newForTest(t, (&fakeSlackServer{}).start(t))
	got := c.FormatText(context.Background(), "see <#C123|general> for details")
	if got != "see #general for details" {
		t.Errorf("FormatText = %q, want %q", got, "see #general for details")
	}
}

func TestFormatText_channelMentionResolvesViaAPI(t *testing.T) {
	srv := (&fakeSlackServer{
		handlers: map[string]http.HandlerFunc{
			"/conversations.info": func(w http.ResponseWriter, r *http.Request) {
				jsonOK(w, map[string]any{
					"channel": map[string]any{
						"id":   "C0ARY9XKNTU",
						"name": "sharing-is-caring",
					},
				})
			},
		},
	}).start(t)

	c := newForTest(t, srv)
	got := c.FormatText(context.Background(), "I started <#C0ARY9XKNTU> yesterday")
	if got != "I started #sharing-is-caring yesterday" {
		t.Errorf("FormatText = %q, want %q", got, "I started #sharing-is-caring yesterday")
	}
}

func TestFormatText_userMentionWithInlineName(t *testing.T) {
	c := newForTest(t, (&fakeSlackServer{}).start(t))
	got := c.FormatText(context.Background(), "cc <@U123|bob>")
	if got != "cc @bob" {
		t.Errorf("FormatText = %q, want %q", got, "cc @bob")
	}
}

func TestFormatText_userMentionResolvesViaAPI(t *testing.T) {
	srv := (&fakeSlackServer{
		handlers: map[string]http.HandlerFunc{
			"/users.info": func(w http.ResponseWriter, r *http.Request) {
				jsonOK(w, map[string]any{
					"user": map[string]any{
						"id": "U999",
						"profile": map[string]any{
							"display_name": "Alice",
							"real_name":    "Alice Example",
						},
					},
				})
			},
		},
	}).start(t)

	c := newForTest(t, srv)
	got := c.FormatText(context.Background(), "hey <@U999> got a sec?")
	if got != "hey @Alice got a sec?" {
		t.Errorf("FormatText = %q, want %q", got, "hey @Alice got a sec?")
	}
}

func TestFormatText_specialBroadcasts(t *testing.T) {
	c := newForTest(t, (&fakeSlackServer{}).start(t))
	cases := map[string]string{
		"<!here> heads up":      "@here heads up",
		"<!channel> announcing": "@channel announcing",
		"<!everyone> notice":    "@everyone notice",
	}
	for in, want := range cases {
		got := c.FormatText(context.Background(), in)
		if got != want {
			t.Errorf("FormatText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatText_userGroupUsesDisplayText(t *testing.T) {
	// <!subteam^S123|@marketing> should render as the display text.
	c := newForTest(t, (&fakeSlackServer{}).start(t))
	got := c.FormatText(context.Background(), "ping <!subteam^S0123|@marketing>")
	if got != "ping @marketing" {
		t.Errorf("FormatText = %q, want %q", got, "ping @marketing")
	}
}

func TestFormatText_dateUsesDisplayText(t *testing.T) {
	// <!date^1392734382^{date_num}|Feb 18, 2014> → Feb 18, 2014
	c := newForTest(t, (&fakeSlackServer{}).start(t))
	got := c.FormatText(context.Background(), "on <!date^1392734382^{date_num}|Feb 18, 2014>")
	if got != "on Feb 18, 2014" {
		t.Errorf("FormatText = %q, want %q", got, "on Feb 18, 2014")
	}
}

func TestFormatText_urlBare(t *testing.T) {
	c := newForTest(t, (&fakeSlackServer{}).start(t))
	got := c.FormatText(context.Background(), "see <https://example.com/path>")
	if got != "see https://example.com/path" {
		t.Errorf("FormatText = %q, want %q", got, "see https://example.com/path")
	}
}

func TestFormatText_urlWithLabel(t *testing.T) {
	c := newForTest(t, (&fakeSlackServer{}).start(t))
	got := c.FormatText(context.Background(), "click <https://example.com|here>")
	if got != "click here" {
		t.Errorf("FormatText = %q, want %q", got, "click here")
	}
}

func TestFormatText_mailto(t *testing.T) {
	c := newForTest(t, (&fakeSlackServer{}).start(t))
	got := c.FormatText(context.Background(), "email <mailto:bob@example.com>")
	if got != "email bob@example.com" {
		t.Errorf("FormatText = %q, want %q", got, "email bob@example.com")
	}
}

func TestFormatText_mixedEntities(t *testing.T) {
	srv := (&fakeSlackServer{
		handlers: map[string]http.HandlerFunc{
			"/conversations.info": func(w http.ResponseWriter, r *http.Request) {
				jsonOK(w, map[string]any{
					"channel": map[string]any{"id": "C1", "name": "general"},
				})
			},
			"/users.info": func(w http.ResponseWriter, r *http.Request) {
				jsonOK(w, map[string]any{
					"user": map[string]any{
						"id": "U1",
						"profile": map[string]any{"display_name": "Carol"},
					},
				})
			},
		},
	}).start(t)

	c := newForTest(t, srv)
	in := "<@U1> started <#C1> — see <https://docs.example.com|the docs>"
	want := "@Carol started #general — see the docs"
	got := c.FormatText(context.Background(), in)
	if got != want {
		t.Errorf("FormatText = %q, want %q", got, want)
	}
}

func TestFormatText_lookupFailureFallsBackToID(t *testing.T) {
	// users.info returns an error; FormatText must not propagate it —
	// instead it should fall back to the raw ID with the @ prefix so the
	// task still gets created.
	srv := (&fakeSlackServer{
		handlers: map[string]http.HandlerFunc{
			"/users.info": func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":false,"error":"user_not_found"}`))
			},
		},
	}).start(t)

	c := newForTest(t, srv)
	got := c.FormatText(context.Background(), "cc <@UDELETED>")
	if got != "cc @UDELETED" {
		t.Errorf("FormatText = %q, want %q", got, "cc @UDELETED")
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
