//go:build linux

package main

import (
	_ "embed"

	"github.com/ebitengine/purego"
)

//go:embed assets/steamworks/libsteam_api.so
var steamAPILibBytes []byte

const steamAPILibFileName = "libsteam_api.so"

func openLibrary(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

func librarySymbol(handle uintptr, name string) (uintptr, error) {
	return purego.Dlsym(handle, name)
}
