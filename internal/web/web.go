// Package web 承载嵌入的管理台静态资源（web/dist，由 Vite 构建产出）。
package web

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:dist
var distFS embed.FS

// Handler 返回 SPA 静态资源处理器，未知路径回退到 index.html。
func Handler() gin.HandlerFunc {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return func(c *gin.Context) {
		path := strings.TrimPrefix(c.Request.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if f, err := sub.Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		// SPA 路由回退
		index, err := sub.Open("index.html")
		if err != nil {
			c.String(http.StatusNotFound, "web UI not built; run: cd web && npm run build")
			return
		}
		defer index.Close()
		stat, _ := index.Stat()
		http.ServeContent(c.Writer, c.Request, "index.html", stat.ModTime(), index.(io.ReadSeeker))
	}
}
