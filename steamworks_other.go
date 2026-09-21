//go:build !windows && !linux

package main

import "fmt"

var steamAPILibBytes []byte

const steamAPILibFileName = ""

func openLibrary(path string) (uintptr, error) {
	return 0, fmt.Errorf("Steam Workshop support is not implemented on this platform")
}

func librarySymbol(handle uintptr, name string) (uintptr, error) {
	return 0, fmt.Errorf("Steam Workshop support is not implemented on this platform")
}
