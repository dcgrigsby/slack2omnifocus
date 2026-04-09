// Command slack2omnifocus turns 👀-reacted Slack messages into OmniFocus
// inbox tasks. See docs/plans/2026-04-09-slack2omnifocus-design.md for
// the full design.
package main

import (
	"context"
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
		if err := runPoll(); err != nil {
			slog.Error("poll failed", slog.Any("error", err))
			os.Exit(1)
		}
	case "doctor":
		if err := runDoctor(); err != nil {
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

func usage() {
	fmt.Fprint(os.Stderr, `slack2omnifocus — turn 👀-reacted Slack messages into OmniFocus inbox tasks

Usage:
  slack2omnifocus poll     run one poll cycle and exit
  slack2omnifocus doctor   sanity-check setup (token, OmniFocus, state file)
  slack2omnifocus help     show this message

Configuration is read from environment variables; create .env.local in
the repo root with `+"`export SLACK_TOKEN=xoxp-...`"+` and let direnv load it,
or use the sh -c wrapper in the provided launchd plist.
`)
}

func runPoll() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	slackClient := slack.New(cfg.SlackToken)
	adapter := &slack.PollAdapter{Client: slackClient}
	runner := omnifocus.OsascriptRunner{}

	statePath, err := defaultStatePath()
	if err != nil {
		return err
	}
	store, err := state.Open(statePath)
	if err != nil {
		return err
	}

	// Bound the poll cycle so a stuck Slack call or a hung osascript
	// cannot outlive its launchd slot. 4 minutes leaves 1 minute of
	// headroom before the next 5-minute cycle fires.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	return poll.Run(ctx, adapter, runner, store, omnifocus.IsRunning)
}

func runDoctor() error {
	failures := 0

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		fmt.Fprintln(os.Stderr, "✗ config:", cfgErr)
		failures++
	} else {
		fmt.Println("✓ SLACK_TOKEN loaded (prefix OK)")

		client := slack.New(cfg.SlackToken)
		userID, err := client.AuthTest(context.Background())
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ slack auth.test:", err)
			failures++
		} else {
			fmt.Printf("✓ Slack auth.test OK (user_id=%s)\n", userID)
		}
	}

	if omnifocus.IsRunning() {
		fmt.Println("✓ OmniFocus is running")
	} else {
		fmt.Println("⚠ OmniFocus is NOT running — poll will skip until it is")
	}

	if statePath, err := defaultStatePath(); err != nil {
		fmt.Fprintln(os.Stderr, "✗ state path:", err)
		failures++
	} else if _, err := state.Open(statePath); err != nil {
		fmt.Fprintln(os.Stderr, "✗ state file:", err)
		failures++
	} else {
		fmt.Printf("✓ State file writable at %s\n", statePath)
	}

	if failures > 0 {
		return fmt.Errorf("%d doctor check(s) failed", failures)
	}
	return nil
}

func defaultStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "slack2omnifocus", "processed.txt"), nil
}
