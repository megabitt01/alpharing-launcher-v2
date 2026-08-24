//go:build !windows

package main

// processRunningUnder is only implemented on Windows; on other platforms
// we fall back to a bounded wait in waitForGameExit instead of detecting
// the actual game process.
func processRunningUnder(dir string) bool {
	return false
}
