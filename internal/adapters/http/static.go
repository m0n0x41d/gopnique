package httpadapter

import "embed"
import "io/fs"

//go:embed static/*
var staticFiles embed.FS

func StaticFiles() fs.FS {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return staticFiles
	}

	return sub
}
