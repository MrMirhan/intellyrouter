//go:build !webui

// Package web holds the dashboard build output for embedding into the binary.
package web

import "io/fs"

// Assets returns nil because this binary was built without the webui tag.
func Assets() fs.FS { return nil }
