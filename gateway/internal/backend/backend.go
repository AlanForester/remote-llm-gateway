// Package backend starts the agent that speaks JSON-RPC on one session.
package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/AlanForester/remote-llm-gateway/internal/procenv"
	"github.com/AlanForester/remote-llm-gateway/internal/proxy"
)

// Agent is one running model session.
type Agent interface {
	Send(ctx context.Context, msg []byte) error
	Recv(ctx context.Context) ([]byte, error)
	Close() error
}

// Config selects how a model name is launched.
type Config struct {
	Echo          bool
	GrokCommand   []string
	ClaudeCommand []string
	OpenAIBase    string
	OpenAIKey     string
	ProxyURL      string
	MCPConfig     string
}

// Start launches the agent for model in dir.
func Start(ctx context.Context, cfg Config, model, dir string) (Agent, error) {
	if cfg.Echo {
		return startEcho(), nil
	}
	kind := kindOf(model)
	switch kind {
	case "claude":
		cmd := cfg.ClaudeCommand
		if len(cmd) == 0 {
			cmd = []string{"claude", "agent", "serve"}
		}
		return startProcess(ctx, cmd, dir, cfg.ProxyURL)
	case "gpt":
		base := cfg.OpenAIBase
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		return startOpenAI(base, cfg.OpenAIKey, model, cfg.ProxyURL, dir, cfg.MCPConfig)
	default:
		cmd := cfg.GrokCommand
		if len(cmd) == 0 {
			cmd = []string{"grok", "agent", "serve"}
		}
		return startProcess(ctx, cmd, dir, cfg.ProxyURL)
	}
}

func kindOf(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "claude"):
		return "claude"
	case strings.Contains(m, "gpt"), strings.Contains(m, "openai"), strings.Contains(m, "codex"):
		return "gpt"
	default:
		return "grok"
	}
}

type lineAgent struct {
	sendFn func(context.Context, []byte) error
	recvFn func(context.Context) ([]byte, error)
	close  func() error
}

func (a *lineAgent) Send(ctx context.Context, msg []byte) error { return a.sendFn(ctx, msg) }
func (a *lineAgent) Recv(ctx context.Context) ([]byte, error)   { return a.recvFn(ctx) }
func (a *lineAgent) Close() error {
	if a.close == nil {
		return nil
	}
	return a.close()
}

func startEcho() Agent {
	replies := make(chan []byte, 8)
	var once sync.Once
	return &lineAgent{
		sendFn: func(_ context.Context, msg []byte) error {
			var in rpcMessage
			if err := json.Unmarshal(msg, &in); err != nil {
				return err
			}
			text := ""
			var params struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(in.Params, &params)
			text = params.Text
			out := rpcMessage{JSONRPC: "2.0", ID: in.ID}
			raw, _ := json.Marshal(map[string]any{
				"text": "echo: " + text,
				"usage": map[string]int{
					"totalTokens": len([]rune(text)),
				},
			})
			out.Result = raw
			b, _ := json.Marshal(out)
			replies <- b
			return nil
		},
		recvFn: func(ctx context.Context) ([]byte, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case b, ok := <-replies:
				if !ok {
					return nil, io.EOF
				}
				return b, nil
			}
		},
		close: func() error {
			once.Do(func() { close(replies) })
			return nil
		},
	}
}

func startProcess(ctx context.Context, command []string, dir, proxyURL string) (Agent, error) {
	if len(command) == 0 {
		return nil, errors.New("empty command")
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = dir
	cmd.Env = proxyEnv(procenv.WithoutGatewaySecrets(os.Environ()), proxyURL)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var mu sync.Mutex
	return &lineAgent{
		sendFn: func(ctx context.Context, msg []byte) error {
			mu.Lock()
			defer mu.Unlock()
			if _, err := stdin.Write(append(bytes.TrimRight(msg, "\n"), '\n')); err != nil {
				return err
			}
			return nil
		},
		recvFn: func(ctx context.Context) ([]byte, error) {
			type result struct {
				b   []byte
				err error
			}
			ch := make(chan result, 1)
			go func() {
				if !sc.Scan() {
					if err := sc.Err(); err != nil {
						ch <- result{err: err}
						return
					}
					ch <- result{err: io.EOF}
					return
				}
				ch <- result{b: append([]byte(nil), sc.Bytes()...)}
			}()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case r := <-ch:
				return r.b, r.err
			}
		},
		close: func() error {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil
		},
	}, nil
}

func proxyEnv(base []string, proxyURL string) []string {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" || !proxy.Valid(proxyURL) {
		return base
	}
	drop := map[string]bool{
		"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true,
		"http_proxy": true, "https_proxy": true, "all_proxy": true,
	}
	out := make([]string, 0, len(base)+6)
	for _, e := range base {
		k, _, _ := strings.Cut(e, "=")
		if drop[k] {
			continue
		}
		out = append(out, e)
	}
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		out = append(out, k+"="+proxyURL)
	}
	return out
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func rpcFail(id any, code int, message string) []byte {
	b, _ := json.Marshal(rpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	})
	return b
}

func proxyTransport(proxyURL string) http.RoundTripper {
	if strings.TrimSpace(proxyURL) == "" {
		return http.DefaultTransport
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return http.DefaultTransport
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{Proxy: http.ProxyURL(u)}
	}
	clone := base.Clone()
	clone.Proxy = http.ProxyURL(u)
	return clone
}
