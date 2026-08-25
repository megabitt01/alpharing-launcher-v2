package main

import (
	"embed"
	"io/fs"
	"net"
	"net/http"
)

// WebKitGTK's <video>/<audio> elements are decoded through a separate
// GStreamer network source, not the normal WebKit resource loader that
// serves images/JS/CSS through the "wails://" custom scheme. That source
// doesn't understand custom app schemes, so video served via wails:// never
// loads on Linux. Serving it over a real loopback HTTP server instead works
// around this.
//
//go:embed frontend/src/assets/bkgVideo.mp4 frontend/src/assets/bkgVideo.webm
var videoAssetsFS embed.FS

func startVideoServer() (string, error) {
	assets, err := fs.Sub(videoAssetsFS, "frontend/src/assets")
	if err != nil {
		return "", err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	server := &http.Server{Handler: http.FileServer(http.FS(assets))}
	go server.Serve(listener)
	return "http://" + listener.Addr().String(), nil
}
