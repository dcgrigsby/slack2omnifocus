// Package slack wraps github.com/slack-go/slack, exposing only the
// operations slack2omnifocus needs and hiding slack-go's types from the
// rest of the codebase.
package slack

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	slackgo "github.com/slack-go/slack"
)

// ReactedMessage is the slack2omnifocus-internal representation of one
// Slack message that the current user has reacted to with the configured reaction.
type ReactedMessage struct {
	Channel      string // e.g. "C024BE91L"
	Timestamp    string // e.g. "1712345678.123456"
	AuthorUserID string // user ID of the message's author
	Text         string // raw message text
}

// Client talks to the Slack Web API with a user OAuth token.
//
// Not safe for concurrent use: the display-name and channel-name caches
// are plain maps with no synchronization. slack2omnifocus's poll loop is
// single-threaded, so a Client instance lives for one poll run and is
// not shared across goroutines.
type Client struct {
	api      *slackgo.Client
	reaction string

	userNameCache    map[string]string
	channelNameCache map[string]string
}

// New returns a Client bound to the real Slack API.
func New(token, reaction string) *Client {
	return &Client{
		api:              slackgo.New(token),
		reaction:         reaction,
		userNameCache:    map[string]string{},
		channelNameCache: map[string]string{},
	}
}

// NewWithURL returns a Client bound to a custom API URL (used by tests).
func NewWithURL(token, apiURL, reaction string) (*Client, error) {
	return &Client{
		api:              slackgo.New(token, slackgo.OptionAPIURL(apiURL)),
		reaction:         reaction,
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

// ListReactions returns every message in the authenticated user's
// reaction history where THAT user's reactions include the configured reaction.
// It paginates through reactions.list.
func (c *Client) ListReactions(ctx context.Context, selfUserID string) ([]ReactedMessage, error) {
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
				if r.Name != c.reaction {
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
	var name string
	switch {
	case ch.IsIM && ch.User != "":
		// DMs have no .Name — use the counterpart's display name instead.
		counterpart, err := c.DisplayName(ctx, ch.User)
		if err != nil {
			return "", fmt.Errorf("resolve DM counterpart for %s: %w", channelID, err)
		}
		name = counterpart
	case ch.Name != "":
		name = ch.Name
	default:
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

// RemoveReaction removes the authenticated user's configured reaction from
// the given message.
func (c *Client) RemoveReaction(ctx context.Context, channel, ts string) error {
	err := c.api.RemoveReactionContext(ctx, c.reaction, slackgo.NewRefToMessage(channel, ts))
	if err != nil {
		return fmt.Errorf("slack reactions.remove: %w", err)
	}
	return nil
}

// slackEntityRe matches Slack's <...> entity references in message text.
// Slack guarantees these are balanced and contain no nested < or >.
// See https://api.slack.com/reference/surfaces/formatting#retrieving-messages
var slackEntityRe = regexp.MustCompile(`<([^<>]+)>`)

// FormatText expands Slack entity references in message text into
// human-readable form:
//
//	<@U123>             → @<display_name>   (via users.info)
//	<@U123|bob>         → @bob              (uses inline label)
//	<#C123>             → #<channel_name>   (via conversations.info)
//	<#C123|general>     → #general          (uses inline label)
//	<!here|here>        → @here
//	<!channel>          → @channel
//	<!subteam^S1|@team> → @team             (uses inline label)
//	<!date^…|Feb 18>    → Feb 18            (uses inline label)
//	<https://url>       → https://url
//	<https://url|text>  → text
//	<mailto:a@b.com>    → a@b.com
//	<mailto:a@b.com|A>  → A
//
// Lookup failures (network, missing scope, deleted user, etc.) fall back
// to the raw ID with a readable prefix (e.g. "@U123" or "#C456") so one
// unresolvable reference never blocks task creation.
func (c *Client) FormatText(ctx context.Context, text string) string {
	return slackEntityRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := match[1 : len(match)-1] // strip < and >
		var label string
		if i := strings.Index(inner, "|"); i >= 0 {
			label = inner[i+1:]
			inner = inner[:i]
		}
		if inner == "" {
			return match
		}
		switch inner[0] {
		case '@':
			if label != "" {
				return "@" + label
			}
			name, err := c.DisplayName(ctx, inner[1:])
			if err != nil {
				return "@" + inner[1:]
			}
			return "@" + name
		case '#':
			if label != "" {
				return "#" + label
			}
			name, err := c.ChannelName(ctx, inner[1:])
			if err != nil {
				return "#" + inner[1:]
			}
			return "#" + name
		case '!':
			if label != "" {
				// User groups, dates, and any other !command with an explicit
				// label — the label already includes any @ prefix it needs.
				return label
			}
			// Bare broadcasts: !here, !channel, !everyone.
			return "@" + inner[1:]
		default:
			// URL or mailto.
			if label != "" {
				return label
			}
			return strings.TrimPrefix(inner, "mailto:")
		}
	})
}
