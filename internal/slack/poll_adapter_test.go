package slack

import (
	"testing"

	"github.com/dcgrigsby/slack2omnifocus/internal/poll"
)

// Compile-time assertion: PollAdapter must satisfy poll.SlackClient.
var _ poll.SlackClient = (*PollAdapter)(nil)

func TestPollAdapter_exists(t *testing.T) {}
