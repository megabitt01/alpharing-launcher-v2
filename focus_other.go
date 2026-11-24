//go:build !windows

package main

// focusWindow is only implemented on Windows, where topmost frontends such
// as Playnite's fullscreen mode can leave a new window without focus.
func focusWindow(title string) {}

// gameFocuser is only implemented on Windows; elsewhere the game is left to
// take focus on its own.
type gameFocuser struct{}

func newGameFocuser() *gameFocuser { return &gameFocuser{} }

func (f *gameFocuser) focusNewWindows(pids []uint32) bool { return false }
