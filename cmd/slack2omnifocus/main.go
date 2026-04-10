// Command slack2omnifocus turns reacted Slack messages into OmniFocus
// inbox tasks. See docs/plans/ for design documents.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/dcgrigsby/slack2omnifocus/internal/config"
	"github.com/dcgrigsby/slack2omnifocus/internal/omnifocus"
	"github.com/dcgrigsby/slack2omnifocus/internal/poll"
	"github.com/dcgrigsby/slack2omnifocus/internal/slack"
	"github.com/dcgrigsby/slack2omnifocus/internal/state"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "poll":
		cfg := parseFlags("poll", os.Args[2:])
		if err := runPoll(cfg); err != nil {
			slog.Error("poll failed", slog.Any("error", err))
			os.Exit(1)
		}
	case "doctor":
		cfg := parseFlags("doctor", os.Args[2:])
		if err := runDoctor(cfg); err != nil {
			slog.Error("doctor failed", slog.Any("error", err))
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func parseFlags(cmd string, args []string) config.Config {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	token := fs.String("token", "", "Slack user OAuth token (xoxp-...)")
	reaction := fs.String("reaction", "", "Slack reaction name to watch (e.g. eyes, white_check_mark)")
	fs.Parse(args)

	cfg, err := config.New(*token, *reaction)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n", err)
		fs.Usage()
		os.Exit(2)
	}
	return cfg
}

func usage() {
	fmt.Fprint(os.Stderr, `slack2omnifocus — turn reacted Slack messages into OmniFocus inbox tasks

Usage:
  slack2omnifocus poll   --token xoxp-... --reaction eyes
  slack2omnifocus doctor --token xoxp-... --reaction eyes
  slack2omnifocus help

Flags:
  --token      Slack user OAuth token (starts with xoxp-)
  --reaction   Slack reaction name to watch (e.g. eyes, white_check_mark)
`)
}

func runPoll(cfg config.Config) error {
	slackClient := slack.New(cfg.Token, cfg.Reaction)
	adapter := &slack.PollAdapter{Client: slackClient}
	runner := omnifocus.OsascriptRunner{}

	sp, err := defaultStatePath(cfg.Token)
	if err != nil {
		return err
	}
	store, err := state.Open(sp)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	return poll.Run(ctx, adapter, runner, store, omnifocus.IsRunning)
}

func runDoctor(cfg config.Config) error {
	failures := 0

	fmt.Println("✓ config validated (token prefix OK, reaction set)")

	client := slack.New(cfg.Token, cfg.Reaction)
	userID, err := client.AuthTest(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ slack auth.test:", err)
		failures++
	} else {
		fmt.Printf("✓ Slack auth.test OK (user_id=%s)\n", userID)
	}

	if omnifocus.IsRunning() {
		fmt.Println("✓ OmniFocus is running")
	} else {
		fmt.Println("⚠ OmniFocus is NOT running — poll will skip until it is")
	}

	sp, err := defaultStatePath(cfg.Token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ state path:", err)
		failures++
	} else if _, err := state.Open(sp); err != nil {
		fmt.Fprintln(os.Stderr, "✗ state file:", err)
		failures++
	} else {
		fmt.Printf("✓ State file writable at %s\n", sp)
	}

	if failures > 0 {
		return fmt.Errorf("%d doctor check(s) failed", failures)
	}
	return nil
}

func defaultStatePath(token string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home dir: %w", err)
	}
	return statePath(home, token), nil
}

func statePath(home, token string) string {
	h := sha256.Sum256([]byte(token))
	tag := hex.EncodeToString(h[:])[:8]
	return filepath.Join(home, ".local", "state", "slack2omnifocus", "processed-"+tag+".txt")
}
