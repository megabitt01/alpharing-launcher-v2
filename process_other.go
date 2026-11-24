//go:build !windows && !linux

package main

// Game process detection is only implemented on Windows and Linux.
func matchingProcesses(dir string) []gameProcess {
	return nil
}
