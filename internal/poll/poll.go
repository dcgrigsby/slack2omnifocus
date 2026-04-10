// Package poll orchestrates one slack2omnifocus poll cycle: list reacted
// Slack messages, create matching OmniFocus inbox tasks, and remove the
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
	ListReactions(ctx context.Context, selfUserID string) ([]SlackMessage, error)
	DisplayName(ctx context.Context, userID string) (string, error)
	ChannelName(ctx context.Context, channelID string) (string, error)
	FormatText(ctx context.Context, text string) string
	Permalink(ctx context.Context, channel, ts string) (string, error)
	RemoveReaction(ctx context.Context, channel, ts string) error
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

	items, err := slack.ListReactions(ctx, selfUserID)
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
		return slack.RemoveReaction(ctx, msg.Channel, msg.Timestamp)
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

	// Expand Slack entity references (<@U…>, <#C…>, <https://…|label>, …)
	// into human-readable text so the task body doesn't contain raw IDs.
	text := slack.FormatText(ctx, msg.Text)

	payload := omnifocus.TaskPayload{
		Name: buildTitle(text),
		Note: buildNote(authorName, channelName, permalink, text),
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

	if err := slack.RemoveReaction(ctx, msg.Channel, msg.Timestamp); err != nil {
		// State is already marked; next poll will retry removal.
		return fmt.Errorf("remove reaction: %w", err)
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
