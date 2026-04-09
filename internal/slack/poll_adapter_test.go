package slack

import (
	"context"
	"net/http"
	"testing"

	"github.com/dcgrigsby/slack2omnifocus/internal/poll"
)

// Compile-time assertion: PollAdapter must satisfy poll.SlackClient.
var _ poll.SlackClient = (*PollAdapter)(nil)

// TestPollAdapter_ListEyesReactions_fieldMapping verifies that the
// ReactedMessage → poll.SlackMessage conversion inside PollAdapter maps
// each field to the correct destination. Distinctive values are used so
// a transposition (e.g. swapping Channel and Timestamp) would fail this
// test even though it would still compile against the interface.
func TestPollAdapter_ListEyesReactions_fieldMapping(t *testing.T) {
	srv := (&fakeSlackServer{
		handlers: map[string]http.HandlerFunc{
			"/reactions.list": func(w http.ResponseWriter, r *http.Request) {
				jsonOK(w, map[string]any{
					"items": []map[string]any{
						{
							"type":    "message",
							"channel": "CHANNEL_VALUE",
							"message": map[string]any{
								"type": "message",
								"ts":   "TIMESTAMP_VALUE",
								"user": "AUTHOR_VALUE",
								"text": "TEXT_VALUE",
								"reactions": []map[string]any{
									{"name": "eyes", "count": 1, "users": []string{"USELF"}},
								},
							},
						},
					},
					"response_metadata": map[string]any{"next_cursor": ""},
				})
			},
		},
	}).start(t)

	client := newForTest(t, srv)
	adapter := &PollAdapter{Client: client}

	msgs, err := adapter.ListEyesReactions(context.Background(), "USELF")
	if err != nil {
		t.Fatalf("ListEyesReactions error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	got := msgs[0]
	if got.Channel != "CHANNEL_VALUE" {
		t.Errorf("Channel = %q, want %q (possible field transposition)", got.Channel, "CHANNEL_VALUE")
	}
	if got.Timestamp != "TIMESTAMP_VALUE" {
		t.Errorf("Timestamp = %q, want %q (possible field transposition)", got.Timestamp, "TIMESTAMP_VALUE")
	}
	if got.AuthorUserID != "AUTHOR_VALUE" {
		t.Errorf("AuthorUserID = %q, want %q (possible field transposition)", got.AuthorUserID, "AUTHOR_VALUE")
	}
	if got.Text != "TEXT_VALUE" {
		t.Errorf("Text = %q, want %q (possible field transposition)", got.Text, "TEXT_VALUE")
	}
}
