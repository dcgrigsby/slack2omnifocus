package omnifocus

import "testing"

func TestIsRunning_returnsBool(t *testing.T) {
	// We can't hermetically assert true or false without controlling the
	// test host, so just verify the call completes and returns without
	// panicking. Any boolean value is acceptable.
	got := IsRunning()
	_ = got
}
