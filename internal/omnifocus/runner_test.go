package omnifocus

import (
	"testing"
)

// Compile-time check: OsascriptRunner must satisfy the Runner interface.
var _ Runner = OsascriptRunner{}

func TestOsascriptRunner_satisfiesInterface(t *testing.T) {
	// Nothing to do at runtime — the compile-time assertion above is the
	// test. This function exists so `go test` has something to call in
	// this file.
}
