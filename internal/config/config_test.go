package config

import (
	"strings"
	"testing"
)

func TestLoad_happyPath(t *testing.T) {
	t.Setenv("SLACK_TOKEN", "xoxp-test-token-123")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.SlackToken != "xoxp-test-token-123" {
		t.Errorf("SlackToken = %q, want %q", cfg.SlackToken, "xoxp-test-token-123")
	}
}

func TestLoad_missingToken(t *testing.T) {
	t.Setenv("SLACK_TOKEN", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with empty SLACK_TOKEN returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "SLACK_TOKEN") {
		t.Errorf("error message does not mention SLACK_TOKEN: %v", err)
	}
}

func TestLoad_wrongTokenPrefix(t *testing.T) {
	t.Setenv("SLACK_TOKEN", "xoxb-bot-token-should-be-rejected")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with xoxb- token returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "xoxp-") {
		t.Errorf("error message does not mention xoxp-: %v", err)
	}
}

func TestLoad_trimsWhitespace(t *testing.T) {
	t.Setenv("SLACK_TOKEN", "  xoxp-padded-token  \n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.SlackToken != "xoxp-padded-token" {
		t.Errorf("SlackToken = %q, want trimmed %q", cfg.SlackToken, "xoxp-padded-token")
	}
}
