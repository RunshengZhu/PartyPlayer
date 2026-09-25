package main

import (
	"embed"
	"io/fs"
)

// webFS 内嵌前端资源，编译后为单可执行文件，无需额外文件。
//
//go:embed web
var webFS embed.FS

func webAssets() fs.FS {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	return sub
}
