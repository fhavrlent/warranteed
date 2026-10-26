// Package web embeds the built frontend (web/dist).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// The translations, shared with the server for reminders and messages.
//
//go:embed src/locales/*.json
var locales embed.FS

// Locales holds one <language>.json file per language.
func Locales() fs.FS {
	sub, err := fs.Sub(locales, "src/locales")
	if err != nil {
		panic(err)
	}
	return sub
}

func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
