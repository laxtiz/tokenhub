package mcp

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"tokenhub/internal/auth"
	"tokenhub/internal/db"
)

// Handler 返回一个 gin.HandlerFunc，把 /mcp 适配为官方 Streamable HTTP 传输。
//
// 设计要点：
//   - 复用 internal/auth.DownstreamAuth，让用户用现有的 Bearer th-xxx 接入；
//   - DownstreamAuth 通过 c.Set("user") / c.Set("downstream_key") 注入身份；
//   - 把身份透传到 transport 的 context.Value，这样 tool handler 才能取出；
//   - SDK 的 StreamableHTTPHandler 内部已经把 server 工厂化（每次会话一个新 Server），
//     这里我们再用一个全局共享 server（无状态工具集会话之间没有共享状态），
//     并通过 wrapContext 把当前请求的 user 注入到 ctx。
func Handler(database *gorm.DB) gin.HandlerFunc {
	impl := &mcp.Implementation{
		Name:    "tokenhub",
		Version: "0.1.0",
	}
	serverOpts := &mcp.ServerOptions{
		Instructions: "TokenHub 网关查询工具集。所有工具都只读，且只返回当前 API Key 所属用户的数据。",
	}
	base := mcp.NewServer(impl, serverOpts)
	registerTools(base)

	// 共享 server，每次 HTTP 会话工厂返回同一个无状态实例。
	getServer := func(_ *http.Request) *mcp.Server { return base }

	streamable := mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{
		// stateless：每个请求独立会话，工具集会话之间不共享状态；
		// 用户身份通过 wrapContext 注入到 ctx。
		Stateless: true,
	})

	return func(c *gin.Context) {
		// 在交给 StreamableHTTPHandler 之前，先跑 DownstreamAuth。
		auth.DownstreamAuth(database)(c)
		if c.IsAborted() {
			return
		}
		u := c.MustGet("user").(*db.User)
		dk, _ := c.Get("downstream_key")
		dkPtr, _ := dk.(*db.DownstreamKey)

		// 把 gin request context 替换为注入身份的 context，
		// 这样 StreamableHTTPHandler → Server.Run → tool handler 都能拿到。
		ctx := withIdentity(c.Request.Context(), u, dkPtr, database)
		c.Request = c.Request.WithContext(ctx)

		// 记录一次访问（按 mcp 子路径打点）
		slog.Debug("mcp request", "user", u.Username, "method", c.Request.Method, "path", c.Request.URL.Path)

		streamable.ServeHTTP(c.Writer, c.Request)
	}
}