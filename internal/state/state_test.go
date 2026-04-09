package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpen_createsMissingDirAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "sub", "processed.txt")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if store.Has("C123", "1.0") {
		t.Errorf("new store unexpectedly reports Has(C123, 1.0) = true")
	}
}

func TestMark_persistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "processed.txt")

	store1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if err := store1.Mark("C123", "1712345678.000001"); err != nil {
		t.Fatalf("Mark() error = %v", err)
	}
	if !store1.Has("C123", "1712345678.000001") {
		t.Error("Has() right after Mark() returned false")
	}

	// Reopen and verify the entry persisted.
	store2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	if !store2.Has("C123", "1712345678.000001") {
		t.Error("Has() after reopen returned false for marked entry")
	}
	if store2.Has("C123", "9999999999.999999") {
		t.Error("Has() returned true for unmarked entry")
	}
}

func TestMark_isIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "processed.txt")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	for i := 0; i < 5; i++ {
		if err := store.Mark("C123", "1.0"); err != nil {
			t.Fatalf("Mark() iteration %d error = %v", i, err)
		}
	}

	// File should contain exactly one line, not five.
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	wantLines := 1
	gotLines := 0
	for _, b := range contents {
		if b == '\n' {
			gotLines++
		}
	}
	if gotLines != wantLines {
		t.Errorf("file has %d lines after %d Mark() calls, want %d\ncontents: %q",
			gotLines, 5, wantLines, contents)
	}
}

func TestMark_multipleDistinctEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "processed.txt")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	entries := [][2]string{
		{"C1", "1.0"},
		{"C1", "2.0"},
		{"C2", "1.0"},
		{"D3", "1712345678.123456"},
	}
	for _, e := range entries {
		if err := store.Mark(e[0], e[1]); err != nil {
			t.Fatalf("Mark(%q, %q) error = %v", e[0], e[1], err)
		}
	}

	for _, e := range entries {
		if !store.Has(e[0], e[1]) {
			t.Errorf("Has(%q, %q) = false after Mark", e[0], e[1])
		}
	}
}
