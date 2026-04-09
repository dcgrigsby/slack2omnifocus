// Package config loads slack2omnifocus runtime configuration from the
// process environment. The .env.local file pattern is intentionally handled
// outside this package (by direnv in a shell or by the launchd wrapper).
// All this package sees is os.Getenv.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Config holds validated runtime configuration.
type Config struct {
	// SlackToken is a Slack user OAuth token (xoxp-...).
	SlackToken string
}

// Load reads configuration from the environment and validates it.
// It returns an error if SLACK_TOKEN is missing or does not look like a
// user token.
func Load() (Config, error) {
	token := strings.TrimSpace(os.Getenv("SLACK_TOKEN"))
	if token == "" {
		return Config{}, errors.New("SLACK_TOKEN environment variable is not set (did you create .env.local and load it via direnv?)")
	}
	if !strings.HasPrefix(token, "xoxp-") {
		return Config{}, fmt.Errorf("SLACK_TOKEN does not look like a user token: expected xoxp- prefix, got %q…", safePrefix(token))
	}
	return Config{SlackToken: token}, nil
}

// safePrefix returns the first few characters of s for use in error messages,
// without leaking the whole token.
func safePrefix(s string) string {
	const n = 5
	if len(s) <= n {
		return s
	}
	return s[:n]
}
