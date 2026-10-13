// Package server is the HTTP and WebSocket front of the gateway.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/AlanForester/remote-llm-gateway/gateway/internal/backend"
	"github.com/AlanForester/remote-llm-gateway/gateway/internal/worktree"
	"github.com/AlanForester/remote-llm-gateway/internal/filesync"
	"github.com/AlanForester/remote-llm-gateway/internal/httpx"
	"github.com/AlanForester/remote-llm-gateway/internal/metrics"
	"github.com/AlanForester/remote-llm-gateway/internal/proxy"
	"github.com/AlanForester/remote-llm-gateway/internal/store"
)

// Options configures a gateway server.
type Options struct {
	Store         *store.Store
	Echo          bool
	WorkRoot      string
	EnvFile       string
	Now           func() time.Time
	GrokCommand   []string
	ClaudeCommand []string
	OpenAIBase    string
	OpenAIKey     string
	MCPConfig     string
}

// Server serves session creation and the ACP WebSocket.
type Server struct {
	opt      Options
	mux      *http.ServeMux
	mu       sync.Mutex
	sessions map[string]*session
	up       websocket.Upgrader
}

type session struct {
	id       string
	userID   string
	model    string
	dir      string
	mu       sync.Mutex
	baseline map[string]string
}

// New builds the gateway handler.
func New(opt Options) *Server {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.OpenAIKey == "" {
		opt.OpenAIKey = os.Getenv("OPENAI_API_KEY")
	}
	if opt.OpenAIBase == "" {
		opt.OpenAIBase = os.Getenv("OPENAI_BASE_URL")
	}
	s := &Server{
		opt:      opt,
		mux:      http.NewServeMux(),
		sessions: map[string]*session{},
		up:       websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
	}
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.Handle("GET /metrics", metrics.Handler())
	s.mux.HandleFunc("GET /v1/me", s.me)
	s.mux.HandleFunc("POST /v1/sessions", s.createSession)
	s.mux.HandleFunc("PUT /v1/sessions/{sessionId}/files", s.putFiles)
	s.mux.HandleFunc("GET /v1/sessions/{sessionId}/changes", s.changes)
	s.mux.HandleFunc("GET /ws/{sessionId}", s.socket)
	return s
}

// Handler is the HTTP handler.
func (s *Server) Handler() http.Handler { return metrics.Instrument("gateway", s.mux) }

type sessionRequest struct {
	Workspace string `json:"workspace"`
	Model     string `json:"model"`
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	user, err := s.opt.Store.Authenticate(httpx.Bearer(r), s.opt.Now())
	if err != nil {
		writeAuth(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, store.Public(user, "", s.opt.Now()))
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	user, err := s.opt.Store.Authenticate(httpx.Bearer(r), s.opt.Now())
	if err != nil {
		writeAuth(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req sessionRequest
	if err := httpx.ReadJSON(r, &req); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_json"})
		return
	}
	if _, err := s.opt.Store.CheckQuota(user.ID, s.opt.Now()); err != nil {
		if errors.Is(err, store.ErrQuota) {
			metrics.QuotaDenied.Inc()
		}
		writeAuth(w, err)
		return
	}
	model, _ := store.ResolveModel(user.ForcedModel, req.Model)
	dir, err := worktree.Prepare(s.opt.WorkRoot, user.ID, req.Workspace)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "worktree"})
		return
	}
	sess := &session{
		id:     newSessionID(),
		userID: user.ID,
		model:  model,
		dir:    dir,
	}
	s.mu.Lock()
	s.sessions[sess.id] = sess
	n := len(s.sessions)
	s.mu.Unlock()
	metrics.Sessions.Set(float64(n))
	view := store.Public(user, req.Model, s.opt.Now())
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"sessionId":  sess.id,
		"model":      view.Model,
		"forced":     view.Forced,
		"tokenLimit": view.TokenLimit,
		"tokensUsed": view.TokensUsed,
		"resetAt":    view.ResetAt,
	})
}

func (s *Server) putFiles(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.openSession(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, filesync.MaxTotalBytes+(1<<20))
	var req struct {
		Files []filesync.File `json:"files"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeSync(w, filesync.ErrTooLarge)
			return
		}
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_json"})
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if err := filesync.Replace(sess.dir, req.Files); err != nil {
		writeSync(w, err)
		return
	}
	base, err := filesync.HashTree(sess.dir)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	sess.baseline = base
	httpx.WriteJSON(w, http.StatusOK, map[string]int{"files": len(req.Files)})
}

func (s *Server) changes(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.openSession(w, r)
	if !ok {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	changed, err := filesync.Diff(sess.dir, sess.baseline)
	if err != nil {
		writeSync(w, err)
		return
	}
	base, err := filesync.HashTree(sess.dir)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	sess.baseline = base
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"files": changed})
}

func (s *Server) openSession(w http.ResponseWriter, r *http.Request) (*session, bool) {
	s.mu.Lock()
	sess := s.sessions[r.PathValue("sessionId")]
	s.mu.Unlock()
	if sess == nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return nil, false
	}
	user, err := s.opt.Store.Authenticate(httpx.Bearer(r), s.opt.Now())
	if err != nil || user.ID != sess.userID {
		writeAuth(w, store.ErrUnauthorized)
		return nil, false
	}
	return sess, true
}

func (s *Server) socket(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionId")
	s.mu.Lock()
	sess := s.sessions[id]
	s.mu.Unlock()
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	user, err := s.opt.Store.Authenticate(httpx.Bearer(r), s.opt.Now())
	if err != nil || user.ID != sess.userID {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(1 << 20)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	adminURL, adminAt, err := s.opt.Store.ProxySettings()
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, rpcError(nil, -32000, err.Error()))
		return
	}
	agent, err := backend.Start(ctx, backend.Config{
		Echo:          s.opt.Echo,
		GrokCommand:   s.opt.GrokCommand,
		ClaudeCommand: s.opt.ClaudeCommand,
		OpenAIBase:    s.opt.OpenAIBase,
		OpenAIKey:     s.opt.OpenAIKey,
		ProxyURL:      proxy.Resolve(adminURL, adminAt, s.opt.EnvFile),
		MCPConfig:     s.opt.MCPConfig,
	}, sess.model, sess.dir)
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, rpcError(nil, -32000, err.Error()))
		return
	}
	defer agent.Close()

	errc := make(chan error, 2)
	go func() {
		errc <- s.clientToAgent(ctx, conn, agent, sess)
	}()
	go func() {
		errc <- s.agentToClient(ctx, conn, agent, sess)
	}()
	<-errc
	cancel()
}

func (s *Server) clientToAgent(ctx context.Context, conn *websocket.Conn, agent backend.Agent, sess *session) error {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if isPrompt(msg) {
			if _, err := s.opt.Store.CheckQuota(sess.userID, s.opt.Now()); err != nil {
				if errors.Is(err, store.ErrQuota) {
					metrics.QuotaDenied.Inc()
				}
				var in struct {
					ID any `json:"id"`
				}
				_ = json.Unmarshal(msg, &in)
				_ = conn.WriteMessage(websocket.TextMessage, rpcError(in.ID, -32001, "Лимит токенов исчерпан"))
				continue
			}
		}
		if err := agent.Send(ctx, msg); err != nil {
			return err
		}
	}
}

func (s *Server) agentToClient(ctx context.Context, conn *websocket.Conn, agent backend.Agent, sess *session) error {
	for {
		msg, err := agent.Recv(ctx)
		if err != nil {
			return err
		}
		if n := usageOf(msg); n > 0 {
			if _, err := s.opt.Store.AddUsage(sess.userID, n, sess.model, s.opt.Now()); err == nil {
				metrics.Tokens.WithLabelValues(sess.model).Add(float64(n))
			} else if errors.Is(err, store.ErrQuota) {
				metrics.QuotaDenied.Inc()
			}
		}
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return err
		}
	}
}

func isPrompt(msg []byte) bool {
	var in struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(msg, &in); err != nil {
		return false
	}
	switch in.Method {
	case "session/prompt", "prompt", "sampling/createMessage":
		return true
	default:
		return false
	}
}

func usageOf(msg []byte) int64 {
	var body struct {
		Result struct {
			Usage struct {
				TotalTokens int64 `json:"totalTokens"`
				Snake       int64 `json:"total_tokens"`
			} `json:"usage"`
		} `json:"result"`
	}
	if err := json.Unmarshal(msg, &body); err != nil {
		return 0
	}
	if body.Result.Usage.TotalTokens > 0 {
		return body.Result.Usage.TotalTokens
	}
	return body.Result.Usage.Snake
}

func rpcError(id any, code int, message string) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
	return b
}

func writeSync(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, filesync.ErrPath):
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_path", "message": err.Error()})
	case errors.Is(err, filesync.ErrTooLarge):
		httpx.WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "too_large", "message": err.Error()})
	default:
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
	}
}

func writeAuth(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrUnauthorized):
		httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	case errors.Is(err, store.ErrQuota):
		httpx.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "quota_exceeded", "message": "Лимит токенов исчерпан"})
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	default:
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
	}
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
