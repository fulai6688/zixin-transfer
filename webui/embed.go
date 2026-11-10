// Package webui embeds the built single page application so the 子欣互传
// binary ships as one self-contained file.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the embedded front-end bundle.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("zixin: embedded assets missing: " + err.Error())
	}
	return sub
}
