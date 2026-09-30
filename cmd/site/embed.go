package main

import (
	"embed"
	"io/fs"
)

//go:embed all:templates
var tmplFS embed.FS

//go:embed all:static
var staticFS embed.FS

// templates returns the FS the html/template engine reads from. Clone a
// sub-directory so callers using ParseFS hit the templates/ root directly.
func templateFS() fs.FS {
	sub, err := fs.Sub(tmplFS, "templates")
	if err != nil {
		panic(err)
	}
	return sub
}

// static returns the FS the docs site ships under /static (style.css,
// favicon). The build copies these into the output root; we don't go via
// embed at request time because the published build is plain static files.
func staticSub() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
