// Package web embeds the built frontend (web/dist).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
