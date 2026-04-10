package main

import (
	"strings"
	"testing"
)

func TestStatePath_includesTokenHash(t *testing.T) {
	p1 := statePath("/fake/home", "xoxp-token-aaa")
	p2 := statePath("/fake/home", "xoxp-token-bbb")

	if p1 == p2 {
		t.Errorf("different tokens should produce different paths, both got %q", p1)
	}
	if !strings.HasPrefix(p1, "/fake/home/.local/state/slack2omnifocus/processed-") {
		t.Errorf("unexpected path prefix: %q", p1)
	}
	if !strings.HasSuffix(p1, ".txt") {
		t.Errorf("path should end with .txt: %q", p1)
	}
}

func TestStatePath_sameTokenSamePath(t *testing.T) {
	p1 := statePath("/fake/home", "xoxp-same-token")
	p2 := statePath("/fake/home", "xoxp-same-token")
	if p1 != p2 {
		t.Errorf("same token should produce same path: %q vs %q", p1, p2)
	}
}
