// Package state manages the on-disk deduplication store that tracks which
// Slack messages have already been turned into OmniFocus tasks. The store is
// a plain text file with one "channel:timestamp" entry per line.
package state

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store is an in-memory set of processed (channel, timestamp) pairs backed
// by a plain-text file. It is not safe for concurrent use across goroutines,
// but slack2omnifocus uses a single goroutine so that's fine.
type Store struct {
	path string
	seen map[string]struct{}
}

// Open reads the state file at path (creating the parent directory if
// necessary) and returns a Store. If the file does not exist, an empty
// Store is returned and the file will be created on the first Mark() call.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create state dir %q: %w", filepath.Dir(path), err)
	}

	seen := make(map[string]struct{})
	f, err := os.Open(path)
	switch {
	case err == nil:
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			seen[line] = struct{}{}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read state file %q: %w", path, err)
		}
	case os.IsNotExist(err):
		// New store, nothing to load.
	default:
		return nil, fmt.Errorf("open state file %q: %w", path, err)
	}

	return &Store{path: path, seen: seen}, nil
}

// Has reports whether the given (channel, ts) pair has already been marked.
func (s *Store) Has(channel, ts string) bool {
	_, ok := s.seen[key(channel, ts)]
	return ok
}

// Mark records a (channel, ts) pair as processed. It appends to the state
// file and fsyncs before returning, so if this call returns nil the entry
// is durable. Mark is idempotent: marking an already-marked pair is a no-op
// and returns nil without writing anything.
func (s *Store) Mark(channel, ts string) error {
	k := key(channel, ts)
	if _, ok := s.seen[k]; ok {
		return nil
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open state file for append: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, k); err != nil {
		return fmt.Errorf("append state entry: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync state file: %w", err)
	}
	s.seen[k] = struct{}{}
	return nil
}

func key(channel, ts string) string {
	return channel + ":" + ts
}
