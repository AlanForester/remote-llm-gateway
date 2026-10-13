package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/AlanForester/remote-llm-gateway/gateway/internal/mcp"
	"github.com/AlanForester/remote-llm-gateway/internal/filesync"
	"github.com/AlanForester/remote-llm-gateway/internal/procenv"
)

func openAITools(hub *mcp.Hub) []map[string]any {
	out := []map[string]any{
		functionTool("read_file", "Прочитать файл в каталоге сессии.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]string{"type": "string", "description": "Относительный путь"},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		}),
		functionTool("write_file", "Записать файл в каталоге сессии.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]string{"type": "string"},
				"content": map[string]string{"type": "string"},
			},
			"required":             []string{"path", "content"},
			"additionalProperties": false,
		}),
		functionTool("list_dir", "Показать файлы в каталоге сессии.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]string{"type": "string", "description": "Относительный путь, точка означает корень"},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		}),
		functionTool("run_command", "Выполнить команду в каталоге сессии.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]string{"type": "string"},
			},
			"required":             []string{"command"},
			"additionalProperties": false,
		}),
	}
	if hub == nil {
		return out
	}
	for _, tool := range hub.Definitions() {
		schema := map[string]any{}
		if len(tool.Schema) > 0 {
			_ = json.Unmarshal(tool.Schema, &schema)
		}
		if schema["type"] == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, functionTool(tool.Name, tool.Description, schema))
	}
	return out
}

func functionTool(name, description string, parameters map[string]any) map[string]any {
	return map[string]any{
		"type":        "function",
		"name":        name,
		"description": description,
		"parameters":  parameters,
	}
}

func runTool(ctx context.Context, dir string, hub *mcp.Hub, name string, args json.RawMessage) string {
	if strings.HasPrefix(name, "mcp_") {
		if hub == nil {
			return "инструмент не найден"
		}
		out, err := hub.Call(ctx, name, args)
		if err != nil {
			return err.Error()
		}
		return out
	}
	var err error
	var out string
	switch name {
	case "read_file":
		out, err = toolRead(dir, args)
	case "write_file":
		out, err = toolWrite(dir, args)
	case "list_dir":
		out, err = toolList(dir, args)
	case "run_command":
		out, err = toolRun(ctx, dir, args)
	default:
		return "инструмент не найден"
	}
	if err != nil {
		return err.Error()
	}
	return out
}

func toolRead(dir string, args json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	full, err := filesync.SafePath(dir, in.Path)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	if len(body) > filesync.MaxFileBytes {
		return "", errors.New("файл слишком большой")
	}
	if !utf8.Valid(body) {
		return "", errors.New("файл не текст")
	}
	return string(body), nil
}

func toolWrite(dir string, args json.RawMessage) (string, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if len(in.Content) > filesync.MaxFileBytes {
		return "", errors.New("файл слишком большой")
	}
	if err := filesync.Apply(dir, []filesync.File{{Path: in.Path, Content: in.Content}}); err != nil {
		return "", err
	}
	return "записан " + in.Path, nil
}

func toolList(dir string, args json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	full, err := sessionPath(dir, in.Path)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return "", err
	}
	if len(entries) > 500 {
		entries = entries[:500]
	}
	var b strings.Builder
	for _, entry := range entries {
		if entry.IsDir() {
			b.WriteString(entry.Name() + "/\n")
			continue
		}
		b.WriteString(entry.Name() + "\n")
	}
	return b.String(), nil
}

func toolRun(ctx context.Context, dir string, args json.RawMessage) (string, error) {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Command) == "" {
		return "", errors.New("пустая команда")
	}
	if len(in.Command) > 4000 {
		return "", errors.New("слишком длинная команда")
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-c", in.Command)
	cmd.Dir = dir
	cmd.Env = procenv.ForTool()
	hardenCommand(cmd)
	var buf bytes.Buffer
	cmd.Stdout = &limitWriter{N: 32 << 10, W: &buf}
	cmd.Stderr = cmd.Stdout
	err := cmd.Run()
	out := buf.String()
	if err != nil {
		if out != "" {
			out += "\n"
		}
		out += err.Error()
	}
	return out, nil
}

func sessionPath(dir, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return filepath.Abs(dir)
	}
	return filesync.SafePath(dir, rel)
}

type limitWriter struct {
	N int
	W io.Writer
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.N <= 0 {
		return len(p), nil
	}
	if len(p) > w.N {
		p = p[:w.N]
	}
	n, err := w.W.Write(p)
	w.N -= n
	if err != nil {
		return n, err
	}
	return len(p), nil
}
