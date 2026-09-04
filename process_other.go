//go:build !windows && !linux

package main

// processRunningUnder is only implemented on Windows and Linux.
func processRunningUnder(dir string) bool {
	return false
}

func matchingProcessNames(dir string) []string {
	return nil
}
