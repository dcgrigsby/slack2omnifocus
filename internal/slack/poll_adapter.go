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
