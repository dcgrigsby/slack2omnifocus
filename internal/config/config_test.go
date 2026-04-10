package config

import (
	"strings"
	"testing"
)

func TestNew_happyPath(t *testing.T) {
	cfg, err := New("xoxp-test-token-123", "eyes")
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	if cfg.Token != "xoxp-test-token-123" {
		t.Errorf("Token = %q, want %q", cfg.Token, "xoxp-test-token-123")
	}
	if cfg.Reaction != "eyes" {
		t.Errorf("Reaction = %q, want %q", cfg.Reaction, "eyes")
	}
}

func TestNew_emptyToken(t *testing.T) {
	_, err := New("", "eyes")
	if err == nil {
		t.Fatal("New() with empty token returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error message does not mention token: %v", err)
	}
}

func TestNew_wrongTokenPrefix(t *testing.T) {
	_, err := New("xoxb-bot-token-should-be-rejected", "eyes")
	if err == nil {
		t.Fatal("New() with xoxb- token returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "xoxp-") {
		t.Errorf("error message does not mention xoxp-: %v", err)
	}
}

func TestNew_emptyReaction(t *testing.T) {
	_, err := New("xoxp-test-token-123", "")
	if err == nil {
		t.Fatal("New() with empty reaction returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "reaction") {
		t.Errorf("error message does not mention reaction: %v", err)
	}
}

func TestNew_trimsWhitespace(t *testing.T) {
	cfg, err := New("  xoxp-padded-token  \n", "  eyes  ")
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	if cfg.Token != "xoxp-padded-token" {
		t.Errorf("Token = %q, want trimmed %q", cfg.Token, "xoxp-padded-token")
	}
	if cfg.Reaction != "eyes" {
		t.Errorf("Reaction = %q, want trimmed %q", cfg.Reaction, "eyes")
	}
}
