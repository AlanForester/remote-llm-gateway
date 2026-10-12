// Package mcp speaks to stdio MCP servers and exposes their tools.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// Server is one stdio MCP process from the config file.
type Server struct {
	Name    string   `json:"name"`
	Command []string `json:"command"`
}

type fileConfig struct {
	Servers []Server `json:"servers"`
}

// Load reads a config file. An empty path means there are no servers.
func Load(path string) ([]Server, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg fileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	for _, srv := range cfg.Servers {
		if strings.TrimSpace(srv.Name) == "" || len(srv.Command) == 0 || strings.TrimSpace(srv.Command[0]) == "" {
			return nil, errors.New("mcp: у сервера нужны name и command")
		}
	}
	return cfg.Servers, nil
}

// Tool is one function the model may call.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// Hub is the set of MCP servers for one session.
type Hub struct {
	procs []*process
	tools map[string]target
}

type target struct {
	proc *process
	name string
}

// Start launches servers and asks each for its tools.
func Start(ctx context.Context, servers []Server) (*Hub, error) {
	h := &Hub{tools: map[string]target{}}
	if len(servers) == 0 {
		return h, nil
	}
	for _, srv := range servers {
		proc, err := startProcess(ctx, srv)
		if err != nil {
			h.Close()
			return nil, err
		}
		h.procs = append(h.procs, proc)
		tools, err := proc.listTools(ctx)
		if err != nil {
			h.Close()
			return nil, fmt.Errorf("mcp %s: %w", srv.Name, err)
		}
		for _, tool := range tools {
			qualified := "mcp_" + safe(srv.Name) + "_" + safe(tool.Name)
			if _, ok := h.tools[qualified]; ok {
				h.Close()
				return nil, fmt.Errorf("mcp: повтор инструмента %s", qualified)
			}
			h.tools[qualified] = target{proc: proc, name: tool.Name}
			tool.Name = qualified
			proc.defs = append(proc.defs, tool)
		}
	}
	return h, nil
}

// Definitions returns OpenAI function tools.
func (h *Hub) Definitions() []Tool {
	if h == nil {
		return nil
	}
	var out []Tool
	for _, proc := range h.procs {
		out = append(out, proc.defs...)
	}
	return out
}

// Call runs one qualified tool name.
func (h *Hub) Call(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if h == nil {
		return "", errors.New("инструмент не найден")
	}
	target, ok := h.tools[name]
	if !ok {
		return "", errors.New("инструмент не найден")
	}
	if len(args) == 0 {
		args = []byte("{}")
	}
	raw, err := target.proc.call(ctx, "tools/call", map[string]any{
		"name":      target.name,
		"arguments": json.RawMessage(args),
	})
	if err != nil {
		return "", err
	}
	return toolText(raw), nil
}

// Close stops every server.
func (h *Hub) Close() {
	if h == nil {
		return
	}
	for _, proc := range h.procs {
		proc.close()
	}
}

type process struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	mu   sync.Mutex
	next int
	defs []Tool
}

func startProcess(ctx context.Context, srv Server) (*process, error) {
	cmd := exec.CommandContext(ctx, srv.Command[0], srv.Command[1:]...)
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
	proc := &process{cmd: cmd, in: stdin, out: bufio.NewReader(stdout), next: 1}
	if _, err := proc.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "remote-llm-gateway", "version": "0.1.0"},
	}); err != nil {
		proc.close()
		return nil, err
	}
	if err := proc.notify("notifications/initialized", map[string]any{}); err != nil {
		proc.close()
		return nil, err
	}
	return proc, nil
}

func (p *process) listTools(ctx context.Context) ([]Tool, error) {
	var out []Tool
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := p.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var page struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, err
		}
		for _, tool := range page.Tools {
			schema := tool.InputSchema
			if len(schema) == 0 {
				schema = []byte(`{"type":"object","properties":{}}`)
			}
			out = append(out, Tool{Name: tool.Name, Description: tool.Description, Schema: schema})
		}
		if page.NextCursor == "" {
			return out, nil
		}
		cursor = page.NextCursor
	}
}

func (p *process) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := p.next
	p.next++
	if err := writeMsg(p.in, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := readMsg(p.out)
		if err != nil {
			return nil, err
		}
		var msg envelope
		if err := json.Unmarshal(raw, &msg); err != nil {
			return nil, err
		}
		if msg.Method != "" && len(msg.ID) > 0 && string(msg.ID) != "null" {
			_ = writeMsg(p.in, map[string]any{
				"jsonrpc": "2.0",
				"id":      msg.ID,
				"error":   map[string]any{"code": -32601, "message": "method not found"},
			})
			continue
		}
		if !sameID(msg.ID, id) {
			continue
		}
		if msg.Error != nil {
			return nil, errors.New(msg.Error.Message)
		}
		if len(msg.Result) == 0 {
			return []byte("{}"), nil
		}
		return msg.Result, nil
	}
}

func (p *process) notify(method string, params any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return writeMsg(p.in, map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
}

func (p *process) close() {
	if p == nil || p.cmd == nil {
		return
	}
	_ = p.in.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	_ = p.cmd.Wait()
}

type envelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func writeMsg(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n", len(body))
	buf.Write(body)
	_, err = w.Write(buf.Bytes())
	return err
}

func readMsg(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, err := strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
			if err != nil {
				return nil, err
			}
			length = n
		}
	}
	if length < 0 || length > 8<<20 {
		return nil, errors.New("mcp: плохой кадр")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func sameID(raw json.RawMessage, want int) bool {
	return string(bytes.TrimSpace(raw)) == strconv.Itoa(want)
}

func safe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "tool"
	}
	return b.String()
}

func toolText(raw json.RawMessage) string {
	var body struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return string(raw)
	}
	var b strings.Builder
	for _, part := range body.Content {
		b.WriteString(part.Text)
	}
	if body.IsError {
		return "ошибка: " + b.String()
	}
	if b.Len() == 0 {
		return string(raw)
	}
	return b.String()
}
