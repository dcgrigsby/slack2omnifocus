// Package config validates slack2omnifocus runtime configuration.
package config

import (
	"errors"
	"fmt"
	"strings"
)

// Config holds validated runtime configuration.
type Config struct {
	// Token is a Slack user OAuth token (xoxp-...).
	Token string
	// Reaction is the Slack reaction name to watch (e.g. "eyes").
	Reaction string
}

// New validates and returns a Config from the given values.
func New(token, reaction string) (Config, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Config{}, errors.New("token is required (pass --token xoxp-...)")
	}
	if !strings.HasPrefix(token, "xoxp-") {
		return Config{}, fmt.Errorf("token does not look like a user token: expected xoxp- prefix, got %q…", safePrefix(token))
	}
	reaction = strings.TrimSpace(reaction)
	if reaction == "" {
		return Config{}, errors.New("reaction is required (pass --reaction <name>)")
	}
	return Config{Token: token, Reaction: reaction}, nil
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
