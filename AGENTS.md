# TokenHub — Agent Instructions

OpenRouter-style LLM gateway in Go (Gin + GORM + pure-Go SQLite, no CGO). Single binary with an embedded Vue3 + Element Plus admin UI (`go:embed`). Downstream clients speak OpenAI (`POST /v1/chat/completions`) or Anthropic (`POST /v1/messages`); upstreams are typed `openai` or `anthropic`. Same-format traffic passes through (model name rewritten, `stream_options.include_usage` injected for OpenAI upstreams); cross-format traffic goes through `internal/convert`.

`README.md` (Chinese) documents feature specs — conversion coverage (tools, images, reasoning), the billing formula, key/channel behavior. Read it before changing convert/billing/relay behavior.

## Commands

Use mise tasks — `mise.toml` manages tool versions (go/node), env vars, and the frontend→backend build order:

```bash
mise run build-web   # frontend → internal/web/dist (skipped when sources unchanged)
mise run build       # go build -o bin/tokenhub ./cmd/server (depends on build-web)
mise run test        # go test ./... (depends on build-web)
mise run dev         # frontend dev server, proxies /api and /v1 to localhost:8080 (run mise run serve in another terminal)
mise run serve       # build then start the gateway (env vars load from .env, template in .env.example; admin/admin123 seeded on first launch)

# Raw commands bypass mise: go build and go test ./... fail on a fresh tree until
# internal/web/dist exists — run `mise run build-web` (or a full npm build) first.
go test ./internal/convert/ ./internal/relay/   # focused tests, no dist dependency
```

No Makefile, CI, or lint config — mise tasks and plain `go build` / `go test`.

## Layout

```
cmd/server/main.go    entry; ALL route wiring lives here (gateway /v1/* → DownstreamAuth, console /api/* → JWT middleware, SPA fallback via NoRoute)
internal/config       env vars: PORT, DB_PATH (default data/tokenhub.db), JWT_SECRET (auto-generated & persisted to settings table), ADMIN_USERNAME/PASSWORD, BODY_LIMIT_MB
internal/db           GORM models, SQLite open (WAL + busy_timeout), async batched LogWriter (channels, don't write log rows synchronously in request path)
internal/auth         JWT for admin console; downstream API keys stored as SHA-256 hashes (plaintext shown once at creation)
internal/convert      OpenAI ↔ Anthropic conversion: request, non-stream response, and a stream state machine (request.go / response.go / stream.go / types.go)
internal/relay        gateway core: channel fallback by priority, key rotation, streaming forward, retry logic
internal/billing      single Cost() function — cache-token pricing
internal/api          admin/user REST handlers (handlers.go, admin.go)
internal/web          go:embed all:dist — the built admin UI; dist is a build artifact, not committed
web/                  Vue3 frontend source (src/api.js centralizes API calls; views in src/views/)
mise.toml             mise: go/node versions, loads .env from repo root (template .env.example, gitignored), task chain build-web → build/test → serve
```

## Architecture rules

- `internal/convert` is pure: no db/GORM/gin dependencies, operates on its own types in `types.go`. Keep it that way; its tests include bidirectional round-trips.
- Only `internal/relay` talks to upstream providers. It imports convert, billing, db. `internal/api` must never call upstreams directly.
- New GORM models/migrations go in `internal/db`; `LogWriter` is the only path for writing request/upstream log rows.

## Invariants (don't break these)

- **Log tables never store message bodies** — request/upstream logs carry metadata + token usage only. Do not add prompt/completion content to logs.
- **`prompt_tokens` is full-input semantics** (includes cache hits); cache read/write are subsets priced separately in `billing.Cost`. Never charge input price on cached tokens.
- **Retry boundary**: silent retry (rotate key / next channel) only before any byte is written downstream. Once streaming has started, send a downstream error frame and stop — never retry.
- **Key state machine**: 401/403 → `invalid` (leaves rotation, manual restore only); 429 → `rate_limited` with 60s cooldown; consecutive failures only count, never remove a key.
- **Model name masking**: every downstream response — including each SSE chunk — must carry the internal downstream model name, never the real upstream model.
- **Provider Base URL convention**: OpenAI-compatible URLs include `/v1` (gateway appends `/chat/completions`); Anthropic-compatible URLs omit it (gateway appends `/v1/messages`).
- **dist is generated, never committed** — there is no `.gitkeep` anymore. `go:embed all:dist` fails to compile when `internal/web/dist` is missing or empty, so backend builds and full `go test ./...` must go through the mise task chain (or a manual frontend build first). `go test ./internal/convert/ ./internal/relay/` has no dist dependency.

## Tests

- `internal/convert/convert_test.go`: pure conversion unit tests + round-trips.
- `internal/relay/relay_test.go`: end-to-end via httptest mock upstreams — pass-through, cross-protocol, streaming, key rotation (401/429), channel fallback, auth failure, unknown model. Extend these when changing relay/convert behavior.

## Conventions

- Comments, error strings, and admin-UI copy are written in **Chinese** — match that in new code.
- Logging uses stdlib `log/slog` (default level debug); gin runs in ReleaseMode.
- A dev SQLite DB lives at `data/tokenhub.db` — don't delete or clobber it.
