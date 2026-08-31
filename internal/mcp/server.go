// Package mcp 把 TokenHub 的网关数据以 MCP 协议暴露给用户自己的 AI agent。
//
// 设计要点：
//   - 只读工具集（5~6 个），全部强制按 user_id 过滤；
//   - 鉴权复用 internal/auth.DownstreamAuth，agent 客户端配置 Bearer th-xxx；
//   - 传输走官方 go-sdk 的 Streamable HTTP，POST/GET/DELETE 同源；
//   - 共享一个无状态 *mcp.Server，把 user/downstream_key/DB 通过 context.Value 注入，
//     让 tool handler 能拿到当前会话身份与共享 *gorm.DB。
package mcp

import (
	"context"
	"log/slog"

	"gorm.io/gorm"

	"tokenhub/internal/db"
)

// ctxKey 用于把当前请求的上下文（用户身份、DB）注入到 tool handler 的 context 中。
type ctxKey string

const (
	ctxKeyUser    ctxKey = "user"
	ctxKeyDownKey ctxKey = "downstream_key"
	ctxKeyDB      ctxKey = "db"
)

// withIdentity 把 user/downstream key/DB 注入到 context，供 tool handler 读取。
func withIdentity(ctx context.Context, u *db.User, dk *db.DownstreamKey, g *gorm.DB) context.Context {
	ctx = context.WithValue(ctx, ctxKeyUser, u)
	ctx = context.WithValue(ctx, ctxKeyDownKey, dk)
	ctx = context.WithValue(ctx, ctxKeyDB, g)
	return ctx
}

// identityFromCtx 工具 handler 调用此函数取出当前请求的身份与共享 DB。
// 若中间件没有注入（理论上不应发生），返回 error。
func identityFromCtx(ctx context.Context) (*db.User, *db.DownstreamKey, *gorm.DB, error) {
	u, ok := ctx.Value(ctxKeyUser).(*db.User)
	if !ok || u == nil {
		return nil, nil, nil, errMissingIdentity
	}
	dk, _ := ctx.Value(ctxKeyDownKey).(*db.DownstreamKey)
	g, _ := ctx.Value(ctxKeyDB).(*gorm.DB)
	if g == nil {
		return nil, nil, nil, errMissingIdentity
	}
	return u, dk, g, nil
}

var errMissingIdentity = &mcpError{msg: "missing identity in context"}

// mcpError 实现 error 同时能被作为 tool result 的 IsError 返回。
type mcpError struct{ msg string }

func (e *mcpError) Error() string { return e.msg }

// debugLog 在 debug 级别打印 MCP 工具调用，便于排查 agent 行为。
func debugLog(tool string, user string, args any) {
	slog.Debug("mcp tool call", "tool", tool, "user", user, "args", args)
}