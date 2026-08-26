//go:build linux

package main

import (
	"os"
	"strconv"
	"strings"
)

// nonGameProcessNames are processes that can carry the SteamAppId
// environment variable without representing the game actually running:
//
//   - steam, steamwebhelper: Steam's own client-wide processes, e.g. while
//     Steam is pre-caching shaders before the game itself has started. They
//     live for the whole Steam session, not just one game launch.
//   - wineserver and the default Windows services wine/Proton starts inside
//     every prefix (services.exe, winedevice.exe, plugplay.exe, explorer.exe):
//     Proton deliberately keeps wineserver (and the prefix's basic Windows
//     services) running in the background after the game exits, or even if
//     the game never actually started, to speed up the *next* launch. This
//     is well-known, intentional Proton/Wine behavior, not a bug - but it
//     means these processes are a poor signal for "is the game running."
//
// Matching any of these would make processRunningUnder report the game as
// running forever, even after a launch is cancelled during shader
// pre-caching (which is exactly what leaves wineserver et al. behind
// without the actual game ever starting).
var nonGameProcessNames = map[string]bool{
	"steam":          true,
	"steamwebhelper": true,
	"wineserver":     true,
	"services.exe":   true,
	"winedevice.exe": true,
	"plugplay.exe":   true,
	"explorer.exe":   true,
}

// matchingProcessNames returns a "comm(pid)" descriptor for every process
// currently making processRunningUnder report the game as running, so
// callers can surface exactly what's being waited on instead of guessing.
func matchingProcessNames(dir string) []string {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	want := "SteamAppId=" + strconv.Itoa(workshopAppID)
	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		comm, err := os.ReadFile("/proc/" + entry.Name() + "/comm")
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(comm))
		if nonGameProcessNames[name] {
			continue
		}
		environ, err := os.ReadFile("/proc/" + entry.Name() + "/environ")
		if err != nil {
			continue
		}
		for _, kv := range strings.Split(string(environ), "\x00") {
			if kv == want {
				matches = append(matches, name+"("+entry.Name()+")")
				break
			}
		}
	}
	return matches
}

// processRunningUnder reports whether MCC is currently running. gamePath's
// Unix path doesn't correspond 1:1 with what Proton/wine report as a
// process's exe or argv (the game runs under a translated Windows path
// inside the compatdata prefix), so instead this checks every process's
// environment for the SteamAppId Steam sets on the whole launch tree -
// reaper, Proton, wineserver, and the game executable itself - while
// excluding processes that aren't a reliable sign of the game actually
// running (see nonGameProcessNames).
func processRunningUnder(dir string) bool {
	return len(matchingProcessNames(dir)) > 0
}
