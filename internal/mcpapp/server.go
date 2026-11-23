package mcpapp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/doctor"
	"github.com/realchendahuang/AgyMate/internal/hub"
	"github.com/realchendahuang/AgyMate/internal/instances"
	"github.com/realchendahuang/AgyMate/internal/rpc"
	"github.com/realchendahuang/AgyMate/internal/telemetry"
	"github.com/realchendahuang/AgyMate/internal/version"
)

const defaultProtocolVersion = "2025-06-18"

// Server serves JSON-RPC 2.0 MCP tools and ui:// resources over stdio.
type Server struct {
	cfgMu     sync.RWMutex
	cfg       config.Config
	client    *http.Client
	store     *telemetry.Store
	rpcClient *rpc.Client
}

// NewServer constructs an AgyMate MCP server using cfg.
func NewServer(cfg config.Config) *Server {
	rpcCli := rpc.NewClient()
	return &Server{
		cfg:       cfg,
		client:    &http.Client{Timeout: 2500 * time.Millisecond},
		store:     telemetry.NewStore(rpcCli.ModelLabel),
		rpcClient: rpcCli,
	}
}

func (s *Server) configCopy() config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

func (s *Server) setConfig(cfg config.Config) {
	s.cfgMu.Lock()
	s.cfg = cfg
	s.cfgMu.Unlock()
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ServeStdio reads newline-delimited JSON-RPC 2.0 messages from stdin and writes responses to stdout.
func (s *Server) ServeStdio(ctx context.Context) error {
	return s.ServeStream(ctx, os.Stdin, os.Stdout)
}

// ServeStream serves MCP JSON-RPC 2.0 over arbitrary reader/writer streams (used by tests and stdio).
func (s *Server) ServeStream(ctx context.Context, r io.Reader, w io.Writer) error {
	reader := bufio.NewReaderSize(r, 1024*1024)
	var writeMu sync.Mutex

	writeResp := func(resp rpcResponse) error {
		b, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		if _, err := w.Write(b); err != nil {
			return err
		}
		_, err = w.Write([]byte("\n"))
		return err
	}

	for {
		if ctx.Err() != nil {
			return nil
		}
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			trimmed := bytes.TrimSpace(line)
			// Skip HTTP-style headers if a host ever emits Content-Length framing, though stdio uses NDJSON.
			if bytes.HasPrefix(trimmed, []byte("{")) {
				var req rpcRequest
				if jerr := json.Unmarshal(trimmed, &req); jerr != nil {
					_ = writeResp(rpcResponse{
						JSONRPC: "2.0",
						ID:      json.RawMessage("null"),
						Error:   &rpcError{Code: -32700, Message: "parse error: " + jerr.Error()},
					})
				} else if resp, hasResp := s.handleRequest(ctx, req); hasResp {
					if werr := writeResp(resp); werr != nil {
						return werr
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func hasMessageID(id json.RawMessage) bool {
	trimmed := bytes.TrimSpace(id)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func (s *Server) handleRequest(ctx context.Context, req rpcRequest) (rpcResponse, bool) {
	isCall := hasMessageID(req.ID)
	switch req.Method {
	case "notifications/initialized", "notifications/cancelled":
		return rpcResponse{}, false
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		protoVer := strings.TrimSpace(params.ProtocolVersion)
		if protoVer == "" {
			protoVer = defaultProtocolVersion
		}
		res := map[string]any{
			"protocolVersion": protoVer,
			"capabilities": map[string]any{
				"tools":     map[string]any{"listChanged": false},
				"resources": map[string]any{"subscribe": false, "listChanged": false},
				"extensions": map[string]any{
					"io.modelcontextprotocol/ui": map[string]any{
						"mimeTypes": []string{AppResourceMimeType},
					},
				},
			},
			"serverInfo": map[string]any{
				"name":    "agymate_hub",
				"title":   "AgyMate",
				"version": version.Version,
			},
			"instructions": "AgyMate 本地遥测、配额监控、Token 账单、历史会话检索与隔离多开管理工具。支持内联卡片、右侧边栏与全页看板。",
		}
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res}, isCall

	case "ping":
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}, isCall

	case "resources/list":
		res := map[string]any{
			"resources": []map[string]any{
				{
					"uri":         AppResourceURI,
					"name":        "AgyMate 控制台",
					"description": "AgyMate 实时配额、Token 账单、会话检索、隔离实例与环境体检交互界面。",
					"mimeType":    AppResourceMimeType,
					"_meta": map[string]any{
						"ui": map[string]any{
							"prefersBorder": false,
						},
					},
				},
			},
		}
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res}, isCall

	case "resources/templates/list":
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"resourceTemplates": []any{}}}, isCall

	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errResp(req.ID, -32602, "invalid params"), isCall
		}
		if params.URI != AppResourceURI {
			return errResp(req.ID, -32002, "resource not found: "+params.URI), isCall
		}
		htmlDoc := LoadAppHTML()
		res := map[string]any{
			"contents": []map[string]any{
				{
					"uri":      AppResourceURI,
					"mimeType": AppResourceMimeType,
					"text":     htmlDoc,
					"_meta": map[string]any{
						"ui": map[string]any{
							"prefersBorder": false,
						},
					},
				},
			},
		}
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res}, isCall

	case "tools/list":
		res := map[string]any{"tools": toolDefinitions()}
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res}, isCall

	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errResp(req.ID, -32602, "invalid tools/call params"), isCall
		}
		result := s.dispatchTool(ctx, params.Name, params.Arguments)
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}, isCall

	default:
		if !isCall {
			return rpcResponse{}, false
		}
		return errResp(req.ID, -32601, "method not found: "+req.Method), true
	}
}

func errResp(id json.RawMessage, code int, msg string) rpcResponse {
	if !hasMessageID(id) {
		id = json.RawMessage("null")
	}
	return rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	}
}

func toolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "open_dashboard",
			"title":       "AgyMate",
			"description": "打开 AgyMate 交互式控制台（可在右侧辅助面板或全页打开，查看账号配额、Token 账单、会话检索、多开实例与体检状态）。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tab": map[string]any{
						"type":        "string",
						"enum":        []string{"overview", "telemetry", "sessions", "profiles", "doctor"},
						"description": "可选默认展示页签（overview / telemetry / sessions / profiles / doctor）",
					},
				},
			},
			"_meta": map[string]any{
				"ui": map[string]any{
					"resourceUri": AppResourceURI,
				},
				"com.google.antigravity/ui": map[string]any{
					"entrypoints": []map[string]any{
						{"type": "aux_pane", "title": "AgyMate"},
						{"type": "global", "title": "AgyMate"},
					},
				},
			},
		},
		{
			"name":        "get_status",
			"title":       "AgyMate 状态总览",
			"description": "获取全部 Antigravity 账号与实例的实时配额余量、重置倒计时、上下文水位、今日等值账单与 Colab 算力概览（在对话中渲染内联卡片）。",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			"_meta": map[string]any{
				"ui": map[string]any{
					"resourceUri": AppResourceURI,
				},
			},
		},
		{
			"name":        "get_tokens",
			"title":       "Token 消耗与账单",
			"description": "从本地 SQLite WAL 只读统计指定时间区间的 Token 消耗、缓存命中率、缓存节省金额、回本倍数与分模型明细。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"preset": map[string]any{
						"type":        "string",
						"enum":        []string{"today", "yesterday", "last_5h", "last_24h", "since_monday", "last_7d", "last_30d", "this_month", "all"},
						"description": "统计区间（默认 today）",
					},
					"mode": map[string]any{
						"type":        "string",
						"enum":        []string{"std", "promo", "nc_std", "nc_promo"},
						"description": "计价口径（默认使用配置项）",
					},
					"instance": map[string]any{
						"type":        "string",
						"description": "按实例过滤（all | main | ide | <profile>）",
					},
				},
			},
		},
		{
			"name":        "search_sessions",
			"title":       "历史会话检索",
			"description": "跨全部 Antigravity 实例对历史对话标题、工作区路径与正文内容进行 FTS5 全文检索。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "检索关键词（留空则返回最近会话）",
					},
					"instance": map[string]any{
						"type":        "string",
						"description": "实例过滤（默认 all）",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "最大返回条数（默认 20）",
					},
				},
			},
		},
		{
			"name":        "run_doctor",
			"title":       "环境健康体检",
			"description": "检查 Antigravity 客户端版本、URL Scheme 路由、Keychain 元数据、隔离实例断链/OAuth 与 Hooks/MCP/Sidecar 集成状态。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"fix": map[string]any{
						"type":        "boolean",
						"description": "是否执行一键无损安全修复（默认 false）",
					},
				},
			},
		},
		{
			"name":        "app_query",
			"title":       "AgyMate UI 查询",
			"description": "内部界面按需数据查询工具（仅限 MCP App 前端调用）。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"section":  map[string]any{"type": "string"},
					"preset":   map[string]any{"type": "string"},
					"mode":     map[string]any{"type": "string"},
					"instance": map[string]any{"type": "string"},
					"query":    map[string]any{"type": "string"},
					"limit":    map[string]any{"type": "integer"},
					"keep":     map[string]any{"type": "integer"},
					"refresh":  map[string]any{"type": "boolean"},
				},
			},
			"_meta": map[string]any{
				"ui": map[string]any{
					"visibility": []string{"app"},
				},
			},
		},
		{
			"name":        "app_action",
			"title":       "AgyMate UI 动作",
			"description": "内部界面交互操作工具（仅限 MCP App 前端调用，受 main/ide 只读保护）。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action":   map[string]any{"type": "string"},
					"instance": map[string]any{"type": "string"},
					"currency": map[string]any{"type": "string"},
				},
			},
			"_meta": map[string]any{
				"ui": map[string]any{
					"visibility": []string{"app"},
				},
			},
		},
	}
}

func (s *Server) dispatchTool(ctx context.Context, name string, args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	switch name {
	case "open_dashboard":
		tab := strArg(args, "tab")
		if tab == "" {
			tab = "overview"
		}
		ov, online := s.fetchOverview(ctx)
		return toolOK(formatOverviewText(ov, online), map[string]any{
			"view":      "dashboard",
			"tab":       tab,
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"overview":  ov,
		})

	case "get_status":
		ov, online := s.fetchOverview(ctx)
		return toolOK(formatOverviewText(ov, online), map[string]any{
			"view":      "inline",
			"tab":       "overview",
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"overview":  ov,
		})

	case "get_tokens":
		preset := strArg(args, "preset")
		if preset == "" {
			preset = "today"
		}
		mode := strArg(args, "mode")
		if mode == "" {
			mode = s.configCopy().Pricing.Mode
		}
		instFilter := strArg(args, "instance")
		if instFilter == "" {
			instFilter = "all"
		}
		rep, online := s.fetchTelemetry(ctx, preset, mode, instFilter)
		return toolOK(formatTelemetryText(rep, s.configCopy().Pricing), map[string]any{
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"telemetry": rep,
		})

	case "search_sessions":
		q := strArg(args, "query")
		instFilter := strArg(args, "instance")
		if instFilter == "" {
			instFilter = "all"
		}
		limit := intArg(args, "limit", 20)
		sessPayload, online := s.fetchSessions(ctx, q, instFilter, limit)
		return toolOK(formatSessionsText(sessPayload.Hits), map[string]any{
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"sessions":  sessPayload,
		})

	case "run_doctor":
		fix := boolArg(args, "fix")
		var fixRep *doctor.FixReport
		var docRep doctor.Report
		var online bool
		if fix {
			fr, dr, on, err := s.runDoctorFix(ctx)
			if err != nil {
				return toolErr("执行安全修复失败: " + err.Error())
			}
			fixRep = &fr
			docRep = dr
			online = on
		} else {
			docRep, online = s.fetchDoctor(ctx)
		}
		return toolOK(formatDoctorText(docRep, fixRep), map[string]any{
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"doctor":    docRep,
			"fix":       fixRep,
		})

	case "app_query":
		return s.handleAppQuery(ctx, args)

	case "app_action":
		return s.handleAppAction(ctx, args)

	default:
		return toolErr("unknown tool: " + name)
	}
}

func (s *Server) handleAppQuery(ctx context.Context, args map[string]any) map[string]any {
	section := strArg(args, "section")
	if section == "" {
		section = "overview"
	}
	switch section {
	case "overview":
		ov, online := s.fetchOverview(ctx)
		return toolOK("ok", map[string]any{
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"overview":  ov,
		})
	case "telemetry":
		preset := strArg(args, "preset")
		if preset == "" {
			preset = "today"
		}
		mode := strArg(args, "mode")
		if mode == "" {
			mode = s.configCopy().Pricing.Mode
		}
		instFilter := strArg(args, "instance")
		if instFilter == "" {
			instFilter = "all"
		}
		rep, online := s.fetchTelemetry(ctx, preset, mode, instFilter)
		return toolOK("ok", map[string]any{
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"telemetry": rep,
		})
	case "sessions":
		q := strArg(args, "query")
		instFilter := strArg(args, "instance")
		if instFilter == "" {
			instFilter = "all"
		}
		limit := intArg(args, "limit", 25)
		sess, online := s.fetchSessions(ctx, q, instFilter, limit)
		return toolOK("ok", map[string]any{
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"sessions":  sess,
		})
	case "doctor":
		doc, online := s.fetchDoctor(ctx)
		return toolOK("ok", map[string]any{
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"doctor":    doc,
		})
	case "cache":
		keep := intArg(args, "keep", 20)
		cache, online := s.fetchCacheScan(ctx, keep)
		return toolOK("ok", map[string]any{
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"cache":     cache,
		})
	default:
		return toolErr("unknown app_query section: " + section)
	}
}

func (s *Server) handleAppAction(ctx context.Context, args map[string]any) map[string]any {
	action := strArg(args, "action")
	instID := strings.TrimSpace(strArg(args, "instance"))
	cfg := s.configCopy()

	switch action {
	case "doctor_fix":
		fr, dr, online, err := s.runDoctorFix(ctx)
		if err != nil {
			return toolErr("安全修复失败: " + err.Error())
		}
		msg := "安全修复完成"
		if len(fr.Fixed) > 0 {
			msg = "已修复：" + strings.Join(fr.Fixed, "；")
		}
		return toolOK(msg, map[string]any{
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"doctor":    dr,
			"fix":       fr,
		})

	case "set_currency":
		cur := config.NormalizeCurrency(strArg(args, "currency"))
		if err := s.updateCurrency(ctx, cur); err != nil {
			return toolErr("切换币种失败: " + err.Error())
		}
		ov, online := s.fetchOverview(ctx)
		return toolOK("已切换币种为 "+cur, map[string]any{
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"overview":  ov,
		})

	case "profile_focus":
		if instID == "" {
			return toolErr("缺少实例 ID")
		}
		if err := s.focusInstance(ctx, cfg, instID); err != nil {
			return toolErr("置前失败: " + err.Error())
		}
		return toolOK("已置前窗口: "+instID, map[string]any{"ok": true})

	case "profile_launch":
		if err := guardProfileLifecycle(instID); err != nil {
			return toolErr(err.Error())
		}
		if err := s.launchProfile(ctx, cfg, instID); err != nil {
			return toolErr("启动失败: " + err.Error())
		}
		ov, online := s.fetchOverview(ctx)
		return toolOK("已启动隔离实例: "+instID, map[string]any{
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"overview":  ov,
		})

	case "profile_stop":
		if err := guardProfileLifecycle(instID); err != nil {
			return toolErr(err.Error())
		}
		if err := s.stopProfile(ctx, cfg, instID); err != nil {
			return toolErr("停止失败: " + err.Error())
		}
		ov, online := s.fetchOverview(ctx)
		return toolOK("已停止隔离实例: "+instID, map[string]any{
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"overview":  ov,
		})

	case "profile_heal":
		if err := guardProfileLifecycle(instID); err != nil {
			return toolErr(err.Error())
		}
		if err := s.healProfile(ctx, cfg, instID); err != nil {
			return toolErr("自愈修复失败: " + err.Error())
		}
		ov, online := s.fetchOverview(ctx)
		return toolOK("已完成实例自愈修复: "+instID, map[string]any{
			"hubUrl":    s.hubURL(),
			"hubOnline": online,
			"overview":  ov,
		})

	default:
		return toolErr("不支持的操作: " + action)
	}
}

func guardProfileLifecycle(id string) error {
	low := strings.ToLower(strings.TrimSpace(id))
	if low == "" {
		return errors.New("缺少实例 ID")
	}
	if low == instances.KindMain || low == instances.KindIDE {
		return instances.ErrReadOnlyLifecycle
	}
	return instances.ValidateProfileName(id)
}

func (s *Server) hubURL() string {
	addr := strings.TrimSpace(s.configCopy().ListenAddr)
	if addr == "" {
		addr = "127.0.0.1:3778"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	return "http://" + addr
}

func (s *Server) hubDo(ctx context.Context, method, path string, body any, dst any) (int, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.hubURL()+path, bodyReader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := config.LoadHubToken(); tok != "" {
		req.Header.Set("X-AgyMate-Token", tok)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		var errObj struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &errObj) == nil && errObj.Error != "" {
			return resp.StatusCode, errors.New(errObj.Error)
		}
		return resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if dst != nil {
		if err := json.Unmarshal(raw, dst); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func (s *Server) fetchOverview(ctx context.Context) (hub.OverviewPayload, bool) {
	var ov hub.OverviewPayload
	if _, err := s.hubDo(ctx, http.MethodGet, "/api/overview", nil, &ov); err == nil {
		return ov, true
	}
	cfg := s.configCopy()
	direct := hub.NewServer(cfg, nil)
	pctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	direct.PollOnce(pctx)
	return direct.BuildOverview(), false
}

func (s *Server) fetchTelemetry(ctx context.Context, preset, mode, instFilter string) (telemetry.TelemetryReport, bool) {
	q := url.Values{}
	q.Set("preset", preset)
	if mode != "" {
		q.Set("mode", mode)
	}
	if instFilter != "" {
		q.Set("instance", instFilter)
	}
	var payload struct {
		Report telemetry.TelemetryReport `json:"report"`
	}
	if _, err := s.hubDo(ctx, http.MethodGet, "/api/telemetry?"+q.Encode(), nil, &payload); err == nil {
		return payload.Report, true
	}

	cfg := s.configCopy()
	instList := instances.DiscoverAll(cfg)
	st := config.LoadState("")
	var subUSD float64
	if cfg.Pricing.UltraMonthlyUSD > 0 {
		subUSD = cfg.Pricing.UltraMonthlyUSD
	} else {
		seenEmail := make(map[string]float64)
		for _, snap := range st.AllAccountSnapshots(time.Now()) {
			em := strings.ToLower(strings.TrimSpace(snap.Email))
			if em == "" {
				continue
			}
			low := strings.ToLower(snap.PlanName + " " + snap.TierID)
			var u float64
			if strings.Contains(low, "ultra") {
				u = 200
			} else if strings.Contains(low, "pro") {
				u = 20
			}
			if u > seenEmail[em] {
				seenEmail[em] = u
			}
		}
		for _, u := range seenEmail {
			subUSD += u
		}
	}
	return s.store.QueryTelemetry(instList, preset, mode, instFilter, subUSD), false
}

type sessionsPayload struct {
	Live     []any                 `json:"live"`
	Contexts []hub.ContextEntry    `json:"contexts,omitempty"`
	Hits     []telemetry.SearchHit `json:"hits"`
}

func (s *Server) fetchSessions(ctx context.Context, query, instFilter string, limit int) (sessionsPayload, bool) {
	q := url.Values{}
	if query != "" {
		q.Set("q", query)
	}
	if instFilter != "" {
		q.Set("instance", instFilter)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	var payload sessionsPayload
	if _, err := s.hubDo(ctx, http.MethodGet, "/api/sessions?"+q.Encode(), nil, &payload); err == nil {
		return payload, true
	}

	cfg := s.configCopy()
	instList := instances.DiscoverAll(cfg)
	hits := s.store.SearchConversations(ctx, instList, query, instFilter, limit)
	return sessionsPayload{
		Live: []any{},
		Hits: hits,
	}, false
}

func (s *Server) fetchDoctor(ctx context.Context) (doctor.Report, bool) {
	var rep doctor.Report
	if _, err := s.hubDo(ctx, http.MethodGet, "/api/doctor", nil, &rep); err == nil {
		return rep, true
	}
	cfg := s.configCopy()
	instList := instances.DiscoverAll(cfg)
	return doctor.RunChecks(cfg, instList, nil), false
}

func (s *Server) runDoctorFix(ctx context.Context) (doctor.FixReport, doctor.Report, bool, error) {
	var payload struct {
		Fix    doctor.FixReport `json:"fix"`
		Doctor doctor.Report    `json:"doctor"`
	}
	if status, err := s.hubDo(ctx, http.MethodPost, "/api/doctor/fix", map[string]any{}, &payload); err == nil {
		return payload.Fix, payload.Doctor, true, nil
	} else if status >= 400 {
		return doctor.FixReport{}, doctor.Report{}, true, err
	}
	cfg := s.configCopy()
	fr := doctor.FixSafe(cfg, "")
	instList := instances.DiscoverAll(cfg)
	dr := doctor.RunChecks(cfg, instList, nil)
	return fr, dr, false, nil
}

func (s *Server) fetchCacheScan(ctx context.Context, keep int) (telemetry.CacheSummary, bool) {
	if keep < 1 {
		keep = 20
	}
	var sum telemetry.CacheSummary
	if _, err := s.hubDo(ctx, http.MethodGet, fmt.Sprintf("/api/cache?keep=%d", keep), nil, &sum); err == nil {
		return sum, true
	}
	cfg := s.configCopy()
	instList := instances.DiscoverAll(cfg)
	return s.store.ScanCache(instList, keep, nil), false
}

func (s *Server) updateCurrency(ctx context.Context, cur string) error {
	upd := map[string]any{
		"pricing": map[string]any{
			"currency": cur,
		},
	}
	cfg := s.configCopy()
	cfg.Pricing.Currency = cur
	s.setConfig(cfg)
	if status, err := s.hubDo(ctx, http.MethodPut, "/api/config", upd, nil); err == nil {
		return nil
	} else if status >= 400 {
		return err
	}
	loaded, err := config.Load()
	if err == nil {
		loaded.Pricing.Currency = cur
		s.setConfig(loaded)
		return config.Save(loaded)
	}
	return config.Save(cfg)
}

func (s *Server) focusInstance(ctx context.Context, cfg config.Config, id string) error {
	if status, err := s.hubDo(ctx, http.MethodPost, "/api/instances/"+url.PathEscape(id)+"/focus", map[string]any{}, nil); err == nil {
		return nil
	} else if status >= 400 {
		return err
	}
	for _, inst := range instances.DiscoverAll(cfg) {
		if inst.ID == id {
			return instances.FocusWindow(inst)
		}
	}
	return fmt.Errorf("未找到实例 %s", id)
}

func (s *Server) launchProfile(ctx context.Context, cfg config.Config, id string) error {
	if status, err := s.hubDo(ctx, http.MethodPost, "/api/instances/"+url.PathEscape(id)+"/launch", map[string]any{}, nil); err == nil {
		return nil
	} else if status >= 400 {
		return err
	}
	_, err := instances.LaunchProfile(cfg, id, "", false)
	return err
}

func (s *Server) stopProfile(ctx context.Context, cfg config.Config, id string) error {
	if status, err := s.hubDo(ctx, http.MethodPost, "/api/instances/"+url.PathEscape(id)+"/stop", map[string]any{}, nil); err == nil {
		return nil
	} else if status >= 400 {
		return err
	}
	return instances.StopProfile(cfg, id, false)
}

func (s *Server) healProfile(ctx context.Context, cfg config.Config, id string) error {
	if status, err := s.hubDo(ctx, http.MethodPost, "/api/instances/"+url.PathEscape(id)+"/heal", map[string]any{}, nil); err == nil {
		return nil
	} else if status >= 400 {
		return err
	}
	_, err := instances.HealProfile(cfg, id, false)
	return err
}

func formatOverviewText(ov hub.OverviewPayload, online bool) string {
	var b strings.Builder
	modeTxt := "Hub 实时"
	if !online {
		modeTxt = "直连采集"
	}
	fmt.Fprintf(&b, "AgyMate 状态总览（%s | 今日等值 %s | 缓存命中 %.1f%% | 节省 %s）\n",
		modeTxt, formatMoneyStr(ov.TodaySummary.TotalUSD, ov.Pricing), ov.TodaySummary.CacheHitRate, formatMoneyStr(ov.TodaySummary.SavedByCacheUSD, ov.Pricing))
	for _, acct := range ov.Accounts {
		slots := make([]string, 0, len(acct.Instances))
		for _, iv := range acct.Instances {
			slots = append(slots, iv.ID)
		}
		quotaTxt := "-"
		for _, bk := range acct.Buckets {
			if bk.Disabled {
				continue
			}
			quotaTxt = fmt.Sprintf("%s %.0f%%", bk.DisplayName, bk.RemainingPct)
			break
		}
		ctxTxt := "-"
		if acct.Context != nil {
			ctxTxt = fmt.Sprintf("%.0f%%", acct.Context.UsagePct)
		}
		fmt.Fprintf(&b, "- %s [%s] | %s | 配额: %s | 上下文: %s | 今日: %s\n",
			acct.DisplayName, strings.Join(slots, "+"), acct.PlanName, quotaTxt, ctxTxt, formatMoneyStr(acct.TodayUSD, ov.Pricing))
	}
	if ov.Colab.Enabled {
		fmt.Fprintf(&b, "Colab 云算力池: %.2f CCU | 速率 %.2f CCU/小时 | 活跃 %d 台\n",
			ov.Colab.CurrentBalance, ov.Colab.ConsumptionRateHourly, ov.Colab.AssignmentsCount)
	}
	return strings.TrimSpace(b.String())
}

func formatTelemetryText(rep telemetry.TelemetryReport, pricing config.PricingConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, "区间: %s | 调用 %d 次 | 总 Token: %d (缓存命中 %.1f%%) | 等值账单: %s | 缓存节省: %s\n",
		rep.PresetLabel, rep.Summary.Calls, rep.Summary.TotalTokens, rep.Summary.CacheHitRate,
		formatMoneyStr(rep.Summary.TotalUSD, pricing), formatMoneyStr(rep.Summary.SavedByCacheUSD, pricing))
	limit := len(rep.ByModel)
	if limit > 12 {
		limit = 12
	}
	for _, m := range rep.ByModel[:limit] {
		fmt.Fprintf(&b, "- %s: %d 次 | 输入 %d | 缓存 %d | 输出 %d | %s\n",
			m.ModelName, m.Calls, m.Input, m.Cache, m.Output, formatMoneyStr(m.TotalUSD, pricing))
	}
	return strings.TrimSpace(b.String())
}

func formatSessionsText(hits []telemetry.SearchHit) string {
	if len(hits) == 0 {
		return "未找到匹配的会话记录。"
	}
	var b strings.Builder
	for _, h := range hits {
		fmt.Fprintf(&b, "- [%s] %s (%s · %d 步 · %s)\n", h.InstanceID, h.Title, h.Workspace, h.StepCount, h.ModifiedAt)
	}
	return strings.TrimSpace(b.String())
}

func formatDoctorText(rep doctor.Report, fix *doctor.FixReport) string {
	var b strings.Builder
	if fix != nil && len(fix.Fixed) > 0 {
		fmt.Fprintf(&b, "安全修复: %s\n", strings.Join(fix.Fixed, "; "))
	}
	fmt.Fprintf(&b, "Antigravity %s", rep.MainVersion)
	if rep.IDEVersion != "" {
		fmt.Fprintf(&b, " | IDE %s", rep.IDEVersion)
	}
	b.WriteByte('\n')
	for _, c := range rep.Checks {
		fmt.Fprintf(&b, "- [%s] %s: %s\n", c.Status, c.Title, c.Summary)
	}
	return strings.TrimSpace(b.String())
}

func formatMoneyStr(usd float64, pricing config.PricingConfig) string {
	if strings.EqualFold(pricing.Currency, "CNY") {
		rate := pricing.USDToCNY
		if rate <= 0 {
			rate = 7.20
		}
		return fmt.Sprintf("¥%.2f", usd*rate)
	}
	return fmt.Sprintf("$%.2f", usd)
}

func toolOK(text string, structured map[string]any) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"structuredContent": structured,
	}
}

func toolErr(msg string) map[string]any {
	return map[string]any{
		"isError": true,
		"content": []map[string]any{
			{"type": "text", "text": msg},
		},
	}
}

func strArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func boolArg(args map[string]any, key string) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return false
	}
	b, _ := v.(bool)
	return b
}

func intArg(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return def
	}
}
