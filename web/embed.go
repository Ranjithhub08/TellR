package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.css app.js favicon.svg fonts/bricolage.woff2
var files embed.FS

func FS() fs.FS {
	return files
}
