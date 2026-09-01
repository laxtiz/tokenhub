package relay

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"tokenhub/internal/billing"
	"tokenhub/internal/convert"
	"tokenhub/internal/db"
)

type Relay struct {
	DB   *gorm.DB
	HTTP *http.Client
	Logs *db.LogWriter

	mu sync.Mutex
	rr map[uint]*atomic.Uint64 // providerID → 轮询计数器
}

func New(database *gorm.DB, logs *db.LogWriter) *Relay {
	return &Relay{
		DB:   database,
		Logs: logs,
		HTTP: &http.Client{
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 300 * time.Second,
				MaxIdleConnsPerHost:   64,
			},
		},
		rr: map[uint]*atomic.Uint64{},
	}
}

func traceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%s-%s", time.Now().Format("20060102150405"), hex.EncodeToString(b[8:]))
}

// Handle 网关入口。downstreamFormat 为 "openai" 或 "anthropic"。
func (r *Relay) Handle(c *gin.Context, downstreamFormat string) {
	start := time.Now()
	user := c.MustGet("user").(*db.User)
	dk := c.MustGet("downstream_key").(*db.DownstreamKey)

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		writeError(c, downstreamFormat, http.StatusBadRequest, "invalid_request", "failed to read body")
		return
	}
	modelName, stream, err := convert.PeekRequest(body)
	if err != nil || modelName == "" {
		writeError(c, downstreamFormat, http.StatusBadRequest, "invalid_request", "missing or invalid model field")
		return
	}
	tid := traceID()
	c.Set("trace_id", tid)

	var m db.Model
	if err := r.DB.Where("name = ? AND disabled = ?", modelName, false).First(&m).Error; err != nil {
		writeError(c, downstreamFormat, http.StatusNotFound, "model_not_found", fmt.Sprintf("model %q not found", modelName))
		return
	}

	type channel struct {
		ch    db.ModelChannel
		prov  db.Provider
		keys  []db.ProviderKey
	}
	var channels []channel
	var chRows []db.ModelChannel
	r.DB.Where("model_id = ? AND disabled = ?", m.ID, false).Order("priority ASC, weight DESC").Find(&chRows)
	for _, chRow := range chRows {
		var prov db.Provider
		if err := r.DB.First(&prov, chRow.ProviderID).Error; err != nil || prov.Disabled {
			continue
		}
		var keys []db.ProviderKey
		r.DB.Where("provider_id = ?", prov.ID).Order("id ASC").Find(&keys)
		channels = append(channels, channel{ch: chRow, prov: prov, keys: keys})
	}
	if len(channels) == 0 {
		writeError(c, downstreamFormat, http.StatusServiceUnavailable, "no_available_channel", "no available upstream channel for model "+modelName)
		return
	}

	var (
		attempt     int
		firstToken  int64 // 首字延迟 ms
		totals      convert.UpstreamUsage
		failSummary []string
	)
	rl := &db.RequestLog{
		TraceID: tid, UserID: user.ID, KeyID: dk.ID,
		DownstreamFormat: downstreamFormat, Model: modelName, Stream: stream,
	}

	for _, ch := range channels {
		for _, key := range r.usableKeys(ch.keys) {
			attempt++
			ul := &db.UpstreamLog{
				TraceID: tid, Attempt: attempt,
				ProviderID: ch.prov.ID, ProviderName: ch.prov.Name, ProviderType: ch.prov.Type,
				KeyID: key.ID, UpstreamModel: ch.ch.UpstreamModel, Stream: stream,
			}
			attemptStart := time.Now()

			upBody, err := buildUpstreamBody(body, downstreamFormat, ch.prov.Type, ch.ch.UpstreamModel, stream)
			if err != nil {
				// 转换失败属客户端请求问题，重试无意义
				ul.StatusCode = 400
				ul.ErrType = "client"
				ul.Error = truncate(err.Error())
				ul.LatencyMS = msSince(attemptStart)
				r.Logs.WriteUpstream(ul)
				r.finishLog(c, rl, attempt, firstToken, totals, failSummary, start)
				writeError(c, downstreamFormat, http.StatusBadRequest, "invalid_request", truncate(err.Error()))
				return
			}

			status, usage, wrote, uerr := r.forwardOnce(c, ch.prov, key.APIKey, upBody, downstreamFormat, modelName, stream, start, &firstToken)
			ul.StatusCode = status
			ul.LatencyMS = msSince(attemptStart)
			if uerr != nil {
				ul.Error = truncate(uerr.err.Error())
				ul.ErrType = uerr.kind
			}
			ul.PromptTokens, ul.CompletionTokens = usage.Prompt, usage.Completion
			ul.CacheReadTokens, ul.CacheWriteTokens = usage.CacheRead, usage.CacheWrite
			r.Logs.WriteUpstream(ul)
			if uerr != nil && uerr.kind == "cancel" {
				// 客户端已中断请求：不做任何重试，也不把中断计入 key 失败
				rl.Status = 499
				rl.AttemptCount = attempt
				rl.LatencyMS = msSince(start)
				rl.FirstTokenMS = firstToken
				rl.Error = truncate(uerr.err.Error())
				r.Logs.WriteRequest(rl)
				return
			}
			r.markKey(&key, status, uerr)
			if uerr != nil {
				slog.Warn("upstream attempt failed",
					"trace_id", tid, "provider", ch.prov.Name, "upstream_model", ch.ch.UpstreamModel,
					"key_id", key.ID, "status", status, "err_type", uerr.kind,
					"error", truncate(uerr.err.Error()))
			}

			if uerr == nil {
				totals = usage
				rl.Status = 200
				rl.AttemptCount = attempt
				rl.PromptTokens, rl.CompletionTokens = usage.Prompt, usage.Completion
				rl.CacheReadTokens, rl.CacheWriteTokens = usage.CacheRead, usage.CacheWrite
				rl.Cost = billing.Cost(&m, usage.Prompt, usage.Completion, usage.CacheRead, usage.CacheWrite)
				rl.LatencyMS = msSince(start)
				rl.FirstTokenMS = firstToken
				r.Logs.WriteRequest(rl)
				r.DB.Model(user).UpdateColumn("spend", gorm.Expr("spend + ?", rl.Cost))
				return
			}

			failSummary = append(failSummary, fmt.Sprintf("%s#%d:%s", ch.prov.Name, key.ID, uerr.kind))
			rl.Error = truncate(strings.Join(failSummary, "; "))
			if wrote {
				// 已开始向下游输出，无法重试
				rl.Status = status
				rl.AttemptCount = attempt
				rl.LatencyMS = msSince(start)
				rl.FirstTokenMS = firstToken
				r.Logs.WriteRequest(rl)
				return
			}
			// 该 key 失败，继续尝试下一个 key / 下一优先级渠道
		}
	}

	rl.Status = http.StatusBadGateway
	rl.AttemptCount = attempt
	rl.LatencyMS = msSince(start)
	r.Logs.WriteRequest(rl)
	lastMsg := "all channels failed"
	if len(failSummary) > 0 {
		lastMsg = "all channels failed: " + strings.Join(failSummary, "; ")
	}
	slog.Error("relay exhausted all channels",
		"trace_id", tid, "model", modelName, "user_id", user.ID, "attempts", attempt)
	writeError(c, downstreamFormat, http.StatusBadGateway, "upstream_error", truncate(lastMsg))
}

func (r *Relay) finishLog(c *gin.Context, rl *db.RequestLog, attempt int, firstToken int64, totals convert.UpstreamUsage, fails []string, start time.Time) {
	rl.Status = http.StatusBadRequest
	rl.AttemptCount = attempt
	rl.FirstTokenMS = firstToken
	rl.PromptTokens, rl.CompletionTokens = totals.Prompt, totals.Completion
	rl.LatencyMS = msSince(start)
	if len(fails) > 0 {
		rl.Error = truncate(strings.Join(fails, "; "))
	}
	r.Logs.WriteRequest(rl)
}

// ---- Key 池 ----

// usableKeys 返回当前可用的 Key（active 或冷却已过的 rate_limited），并以轮询起点排序。
func (r *Relay) usableKeys(keys []db.ProviderKey) []db.ProviderKey {
	now := time.Now()
	var out []db.ProviderKey
	for _, k := range keys {
		switch k.Status {
		case "active":
			out = append(out, k)
		case "rate_limited":
			if k.CooldownUntil == nil || now.After(*k.CooldownUntil) {
				out = append(out, k)
			}
		}
	}
	if len(out) > 1 {
		start := int(r.nextRR(keys[0].ProviderID)) % len(out)
		out = append(out[start:], out[:start]...)
	}
	return out
}

func (r *Relay) nextRR(providerID uint) uint64 {
	r.mu.Lock()
	counter, ok := r.rr[providerID]
	if !ok {
		counter = &atomic.Uint64{}
		r.rr[providerID] = counter
	}
	r.mu.Unlock()
	return counter.Add(1)
}

func (r *Relay) markKey(k *db.ProviderKey, status int, uerr *upstreamErr) {
	updates := map[string]any{"last_used_at": time.Now()}
	switch {
	case uerr == nil:
		updates["consecutive_fails"] = 0
		if k.Status == "rate_limited" {
			updates["status"] = "active"
			slog.Info("provider key recovered", "key_id", k.ID, "provider_id", k.ProviderID)
		}
	case uerr.kind == "auth":
		updates["status"] = "invalid"
		updates["last_error"] = truncate(uerr.err.Error())
		slog.Warn("provider key marked invalid (auth failure)", "key_id", k.ID, "provider_id", k.ProviderID)
	case uerr.kind == "rate_limit":
		cd := time.Now().Add(60 * time.Second)
		updates["status"] = "rate_limited"
		updates["cooldown_until"] = &cd
		updates["last_error"] = truncate(uerr.err.Error())
		slog.Warn("provider key cooling down (rate limited)", "key_id", k.ID, "provider_id", k.ProviderID, "until", cd.Format(time.RFC3339))
	case uerr.kind == "server" || uerr.kind == "timeout" || uerr.kind == "network":
		updates["consecutive_fails"] = k.ConsecutiveFails + 1
		updates["last_error"] = truncate(uerr.err.Error())
	}
	r.DB.Model(&db.ProviderKey{}).Where("id = ?", k.ID).Updates(updates)
}

// ---- 上游请求构造 ----

type upstreamErr struct {
	kind string // auth | rate_limit | server | timeout | network | client | midstream
	err  error
}

// buildUpstreamBody 生成上游请求体：同格式透传（改写 model 并注入 usage 参数），异格式转换。
func buildUpstreamBody(body []byte, downFormat, upFormat, upstreamModel string, stream bool) ([]byte, error) {
	if downFormat == upFormat {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(body, &m); err != nil {
			return nil, errors.New("invalid JSON body")
		}
		m["model"], _ = json.Marshal(upstreamModel)
		if upFormat == "openai" && stream {
			// 注入 stream_options.include_usage 以便流式计费
			if _, ok := m["stream_options"]; !ok {
				m["stream_options"], _ = json.Marshal(convert.OpenAIStreamOptions{IncludeUsage: true})
			}
		}
		return json.Marshal(m)
	}
	if upFormat == "openai" { // down=anthropic → up=openai
		var ar convert.AnthropicRequest
		if err := json.Unmarshal(body, &ar); err != nil {
			return nil, fmt.Errorf("invalid anthropic request: %w", err)
		}
		ar.Model = upstreamModel
		or, err := convert.AnthropicToOpenAIRequest(&ar)
		if err != nil {
			return nil, err
		}
		return json.Marshal(or)
	}
	// down=openai → up=anthropic
	var or convert.OpenAIRequest
	if err := json.Unmarshal(body, &or); err != nil {
		return nil, fmt.Errorf("invalid openai request: %w", err)
	}
	or.Model = upstreamModel
	ar, err := convert.OpenAIToAnthropicRequest(&or)
	if err != nil {
		return nil, err
	}
	return json.Marshal(ar)
}

// upstreamURL 按 SDK 通用规范拼接上游地址：
// openai 型 BaseURL 含 /v1 后缀（如 https://api.openai.com/v1），内部补 /chat/completions；
// anthropic 型 BaseURL 不带 /v1（如 https://api.anthropic.com），内部补 /v1/messages。
func upstreamURL(base, upFormat string) string {
	base = strings.TrimRight(base, "/")
	if upFormat == "anthropic" {
		return base + "/v1/messages"
	}
	return base + "/chat/completions"
}

func (r *Relay) newUpstreamRequest(ctx context.Context, prov db.Provider, apiKey string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL(prov.BaseURL, prov.Type), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if prov.Type == "anthropic" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	applyProviderHeaders(req, prov)
	return req, nil
}

// applyProviderHeaders 在标准头设定后覆盖 User-Agent 与附加自定义 headers。
// 自定义 headers 不能覆盖鉴权头；此处只 Append 未经保护的 key 以保证鉴权优先。
func applyProviderHeaders(req *http.Request, prov db.Provider) {
	if ua := strings.TrimSpace(prov.UserAgent); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if prov.CustomHeaders == "" {
		return
	}
	var extra map[string]string
	if err := json.Unmarshal([]byte(prov.CustomHeaders), &extra); err != nil {
		slog.Warn("provider custom_headers parse failed", "provider_id", prov.ID, "error", err)
		return
	}
	for k, v := range extra {
		lk := strings.ToLower(k)
		// 防御性二次校验（admin 层 validateCustomHeaders 已禁过这些，但 DB 旧数据可能遗留）
		switch lk {
		case "authorization", "x-api-key", "anthropic-version", "host",
			"content-length", "content-type", "accept":
			slog.Warn("custom header ignored (reserved)", "provider_id", prov.ID, "header", lk)
			continue
		}
		req.Header.Set(k, v)
	}
}

// ---- 转发 ----

// forwardOnce 发起一次上游请求并中继响应。成功时 usage 返回上游用量；
// wrote 表示已向下游写出字节。失败时返回分类错误。
func (r *Relay) forwardOnce(
	c *gin.Context, prov db.Provider, apiKey string,
	upBody []byte, downFormat, downstreamModel string, stream bool, start time.Time, firstToken *int64,
) (status int, usage convert.UpstreamUsage, wrote bool, uerr *upstreamErr) {
	req, err := r.newUpstreamRequest(c.Request.Context(), prov, apiKey, upBody)
	if err != nil {
		return 0, usage, wrote, &upstreamErr{kind: "network", err: err}
	}
	resp, err := r.HTTP.Do(req)
	if err != nil {
		if canceledByClient(c, err) {
			return 0, usage, wrote, &upstreamErr{kind: "cancel", err: context.Canceled}
		}
		if isTimeout(err) {
			return 0, usage, wrote, &upstreamErr{kind: "timeout", err: err}
		}
		return 0, usage, wrote, &upstreamErr{kind: "network", err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return resp.StatusCode, usage, wrote, &upstreamErr{kind: classifyStatus(resp.StatusCode), err: errors.New(truncate(string(b)))}
	}

	if !stream {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			if canceledByClient(c, err) {
				return resp.StatusCode, usage, wrote, &upstreamErr{kind: "cancel", err: context.Canceled}
			}
			return resp.StatusCode, usage, wrote, &upstreamErr{kind: "network", err: err}
		}
		usage, _ = extractUsage(prov.Type, b)
		out := b
		if prov.Type != downFormat {
			converted, cerr := convertResponseBody(prov.Type, downFormat, b)
			if cerr != nil {
				return resp.StatusCode, usage, wrote, &upstreamErr{kind: "server", err: cerr}
			}
			out = converted
		}
		// 下游看到的 model 一律为内部设定的下游模型名
		out = rewriteModelField(out, downstreamModel)
		c.Data(http.StatusOK, "application/json", out)
		return resp.StatusCode, usage, true, nil
	}

	// 流式中继
	setSSEHeaders(c)
	w := c.Writer
	flusher, _ := w.(http.Flusher)
	markFirstToken := func() {
		if *firstToken == 0 {
			*firstToken = msSince(start)
		}
	}

	if prov.Type == downFormat {
		// 同格式透传 + 观测用量
		usageObs := newUsageObserver(prov.Type)
		sawDone := false
		br := bufio.NewReaderSize(resp.Body, 64<<10)
		for {
			event, data, err := nextSSE(br)
			if err != nil {
				break
			}
			usageObs.Observe(event, data)
			// 透传时把每个 chunk 的 model 改写为下游模型名
			if strings.Contains(data, `"model"`) {
				data = string(rewriteModelField([]byte(data), downstreamModel))
			}
			if werr := writeSSE(w, event, data); werr != nil {
				return resp.StatusCode, usageObs.U(), true, &upstreamErr{kind: "midstream", err: werr}
			}
			markFirstToken()
			if flusher != nil {
				flusher.Flush()
			}
			if data == "[DONE]" {
				sawDone = true
				break
			}
		}
		usage = usageObs.U()
		// 上游异常断流时为 OpenAI 下游补齐结束帧
		if !sawDone && downFormat == "openai" {
			w.Write([]byte("data: [DONE]\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
		return resp.StatusCode, usage, true, nil
	}

	// 跨格式流式转换（model 直接由转换器输出为下游模型名）
	xf := convert.NewXformer(prov.Type, downFormat, downstreamModel)
	br := bufio.NewReaderSize(resp.Body, 64<<10)
	sawDone := false
	for {
		event, data, err := nextSSE(br)
		if err != nil {
			break
		}
		out, done, terr := xf.Transform(event, data)
		if terr != nil {
			// 上游流内报错：向下游发错误帧
			writeSSE(w, "error", errorFrame(downFormat, terr.Error()))
			if flusher != nil {
				flusher.Flush()
			}
			return resp.StatusCode, usage, true, &upstreamErr{kind: "midstream", err: terr}
		}
		if len(out) > 0 {
			if _, werr := w.Write(out); werr != nil {
				return resp.StatusCode, usage, true, &upstreamErr{kind: "midstream", err: werr}
			}
			markFirstToken()
			if flusher != nil {
				flusher.Flush()
			}
		}
		if done {
			sawDone = true
			break
		}
	}
	if !sawDone {
		// 上游异常断流，补齐下游收尾帧
		if tail := xf.Finish(); len(tail) > 0 {
			w.Write(tail)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
	if ug, ok := xf.(interface{ Usage() convert.UpstreamUsage }); ok {
		usage = ug.Usage()
	}
	return resp.StatusCode, usage, true, nil
}

// ---- SSE 基础设施 ----

func setSSEHeaders(c *gin.Context) {
	h := c.Writer.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	if f, ok := c.Writer.(http.Flusher); ok {
		f.Flush()
	}
}

// nextSSE 读取一个 SSE 事件，返回 event 名与 data 内容。
func nextSSE(br *bufio.Reader) (event, data string, err error) {
	var ev, sb strings.Builder
	for {
		line, rerr := br.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		switch {
		case rerr != nil && rerr != io.EOF:
			return "", "", rerr
		case trimmed == "":
			if sb.Len() > 0 || ev.Len() > 0 {
				return ev.String(), sb.String(), nil
			}
			if rerr == io.EOF {
				return "", "", io.EOF
			}
			continue
		}
		if strings.HasPrefix(trimmed, "event:") {
			ev.WriteString(strings.TrimSpace(strings.TrimPrefix(trimmed, "event:")))
		} else if strings.HasPrefix(trimmed, "data:") {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(strings.TrimPrefix(strings.TrimPrefix(trimmed, "data:"), " "))
		}
		if rerr == io.EOF {
			if sb.Len() > 0 || ev.Len() > 0 {
				return ev.String(), sb.String(), nil
			}
			return "", "", io.EOF
		}
	}
}

func writeSSE(w io.Writer, event, data string) error {
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return err
		}
		return nil
	}
	_, err := fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

// ---- 用量 / 错误辅助 ----

type usageObserver struct {
	kind string
	open *convert.OpenAIStreamUsage
	anth *convert.AnthropicStreamUsage
}

func newUsageObserver(upFormat string) *usageObserver {
	o := &usageObserver{kind: upFormat}
	if upFormat == "openai" {
		o.open = &convert.OpenAIStreamUsage{}
	} else {
		o.anth = &convert.AnthropicStreamUsage{}
	}
	return o
}

func (o *usageObserver) Observe(event, data string) {
	if o.open != nil {
		o.open.Observe(data)
	} else if o.anth != nil {
		o.anth.Observe(event, data)
	}
}

func (o *usageObserver) U() convert.UpstreamUsage {
	if o.open != nil {
		return o.open.U
	}
	return o.anth.U
}

func extractUsage(upFormat string, body []byte) (convert.UpstreamUsage, bool) {
	if upFormat == "openai" {
		return convert.ParseOpenAIUsage(body)
	}
	return convert.ParseAnthropicUsage(body)
}

// convertResponseBody 非流式跨格式响应转换。
func convertResponseBody(upFormat, downFormat string, body []byte) ([]byte, error) {
	if upFormat == "anthropic" { // → openai
		var ar convert.AnthropicResponse
		if err := json.Unmarshal(body, &ar); err != nil {
			return nil, fmt.Errorf("invalid upstream anthropic response: %w", err)
		}
		out, err := json.Marshal(convert.AnthropicToOpenAIResponse(&ar))
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	var or convert.OpenAIResponse
	if err := json.Unmarshal(body, &or); err != nil {
		return nil, fmt.Errorf("invalid upstream openai response: %w", err)
	}
	return json.Marshal(convert.OpenAIToAnthropicResponse(&or))
}

// rewriteModelField 把 JSON 响应体中的 model 字段改写为下游模型名；解析失败时原样返回。
func rewriteModelField(body []byte, model string) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	if _, ok := m["model"]; !ok {
		return body
	}
	m["model"], _ = json.Marshal(model)
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

func classifyStatus(status int) string {
	switch {
	case status == 401 || status == 403:
		return "auth"
	case status == 402 || status == 429:
		return "rate_limit"
	case status >= 500:
		return "server"
	default:
		return "client"
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return false
}

// canceledByClient 判断上游请求失败是否因客户端断开（请求 context 被取消）导致。
func canceledByClient(c *gin.Context, err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	return errors.Is(c.Request.Context().Err(), context.Canceled)
}

func errorFrame(downFormat, msg string) string {
	if downFormat == "anthropic" {
		return string(convert.AnthropicErrorBodyJSON("api_error", msg))
	}
	return string(convert.OpenAIErrorBodyJSON("api_error", msg))
}

func writeError(c *gin.Context, downFormat string, status int, typ, msg string) {
	if downFormat == "anthropic" {
		c.Data(status, "application/json", convert.AnthropicErrorBodyJSON(typ, msg))
		return
	}
	c.Data(status, "application/json", convert.OpenAIErrorBodyJSON(typ, msg))
}

func truncate(s string) string {
	if len(s) > 900 {
		return s[:900]
	}
	return s
}

func msSince(t time.Time) int64 {
	return time.Since(t).Milliseconds()
}
