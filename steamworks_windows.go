//go:build windows

package main

import (
	_ "embed"

	"golang.org/x/sys/windows"
)

//go:embed assets/steamworks/steam_api64.dll
var steamAPILibBytes []byte

const steamAPILibFileName = "steam_api64.dll"

func openLibrary(path string) (uintptr, error) {
	handle, err := windows.LoadLibrary(path)
	return uintptr(handle), err
}

func librarySymbol(handle uintptr, name string) (uintptr, error) {
	return windows.GetProcAddress(windows.Handle(handle), name)
}
