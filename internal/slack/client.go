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
