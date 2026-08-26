//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func processImagePath(pid uint32) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:size]), nil
}

// matchingProcessNames returns the image path of every running process
// whose executable resides inside dir, e.g. the game itself or its
// anti-cheat launcher.
func matchingProcessNames(dir string) []string {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)

	prefix := strings.ToLower(filepath.Clean(dir)) + string(filepath.Separator)
	var matches []string
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		path, imgErr := processImagePath(entry.ProcessID)
		if imgErr != nil {
			continue
		}
		if strings.HasPrefix(strings.ToLower(filepath.Clean(path)), prefix) {
			matches = append(matches, path)
		}
	}
	return matches
}

// processRunningUnder reports whether any running process's executable
// resides inside dir, e.g. the game itself or its anti-cheat launcher.
func processRunningUnder(dir string) bool {
	return len(matchingProcessNames(dir)) > 0
}
