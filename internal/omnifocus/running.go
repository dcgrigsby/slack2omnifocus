package omnifocus

import "os/exec"

// IsRunning reports whether OmniFocus is currently running.
//
// It uses `pgrep -x OmniFocus`, which exits 0 if any process matches and
// non-zero otherwise. This check is deliberate: we do NOT use
// `tell application "OmniFocus" to ...` because that auto-launches the app
// as a side effect, and the whole point of this check is to avoid auto-launch.
func IsRunning() bool {
	return exec.Command("/usr/bin/pgrep", "-x", "OmniFocus").Run() == nil
}
