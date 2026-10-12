package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/AlanForester/remote-llm-gateway/gateway/internal/mcp"
)

const maxToolRounds = 8

func startOpenAI(base, key, model, proxyURL, dir, mcpConfig string) (Agent, error) {
	servers, err := mcp.Load(mcpConfig)
	if err != nil {
		return nil, err
	}
	hub, err := mcp.Start(context.Background(), servers)
	if err != nil {
		return nil, err
	}
	replies := make(chan []byte, 4)
	client := &http.Client{Transport: proxyTransport(proxyURL)}
	return &lineAgent{
		sendFn: func(ctx context.Context, msg []byte) error {
			var in rpcMessage
			if err := json.Unmarshal(msg, &in); err != nil {
				return err
			}
			if !isPromptMethod(in.Method) {
				replies <- rpcResult(in.ID, map[string]any{})
				return nil
			}
			if key == "" {
				replies <- rpcFail(in.ID, -32000, "OPENAI_API_KEY не задан")
				return nil
			}
			var params struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(in.Params, &params)
			text, tokens, err := toolLoop(ctx, client, base, key, model, dir, hub, params.Text)
			if err != nil {
				replies <- rpcFail(in.ID, -32000, err.Error())
				return nil
			}
			replies <- rpcResult(in.ID, map[string]any{
				"text":  text,
				"usage": map[string]int64{"totalTokens": tokens},
			})
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
			hub.Close()
			return nil
		},
	}, nil
}

func isPromptMethod(method string) bool {
	switch method {
	case "", "session/prompt", "prompt", "sampling/createMessage":
		return true
	default:
		return false
	}
}

func toolLoop(ctx context.Context, client *http.Client, base, key, model, dir string, hub *mcp.Hub, text string) (string, int64, error) {
	input := []any{
		map[string]any{"role": "user", "content": text},
	}
	tools := openAITools(hub)
	var prev string
	var answer string
	var tokens int64
	for round := 0; round < maxToolRounds; round++ {
		body := map[string]any{
			"model": model,
			"input": input,
			"tools": tools,
		}
		if prev != "" {
			body["previous_response_id"] = prev
			body["input"] = input
		}
		raw, err := postOpenAI(ctx, client, base, key, body)
		if err != nil {
			return "", tokens, err
		}
		parsed, err := parseOpenAI(raw)
		if err != nil {
			return "", tokens, err
		}
		tokens += parsed.tokens
		if parsed.text != "" {
			answer = parsed.text
		}
		if len(parsed.calls) == 0 {
			return answer, tokens, nil
		}
		outputs := make([]any, 0, len(parsed.calls))
		for _, call := range parsed.calls {
			outputs = append(outputs, map[string]any{
				"type":    "function_call_output",
				"call_id": call.id,
				"output":  runTool(ctx, dir, hub, call.name, call.args),
			})
		}
		if parsed.id != "" {
			prev = parsed.id
			input = outputs
			continue
		}
		input = append(input, outputs...)
	}
	if answer == "" {
		answer = "Остановлено после лимита вызовов инструментов."
	}
	return answer, tokens, nil
}

func postOpenAI(ctx context.Context, client *http.Client, base, key string, body any) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/responses", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, errStatus(res.Status)
	}
	return payload, nil
}

func errStatus(status string) error {
	return &statusError{status: status}
}

type statusError struct{ status string }

func (e *statusError) Error() string { return "openai: " + e.status }

type openAICall struct {
	id   string
	name string
	args json.RawMessage
}

type openAIParsed struct {
	id     string
	text   string
	tokens int64
	calls  []openAICall
}

func parseOpenAI(raw []byte) (openAIParsed, error) {
	var body struct {
		ID         string `json:"id"`
		OutputText string `json:"output_text"`
		Output     []struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			CallID    string          `json:"call_id"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Content   []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			TotalTokens int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return openAIParsed{}, err
	}
	parsed := openAIParsed{id: body.ID, text: body.OutputText, tokens: body.Usage.TotalTokens}
	var b strings.Builder
	for _, item := range body.Output {
		if item.Type == "function_call" {
			id := item.CallID
			if id == "" {
				id = item.ID
			}
			parsed.calls = append(parsed.calls, openAICall{id: id, name: item.Name, args: argsOf(item.Arguments)})
			continue
		}
		for _, part := range item.Content {
			b.WriteString(part.Text)
		}
	}
	if parsed.text == "" {
		parsed.text = b.String()
	}
	return parsed, nil
}

func argsOf(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []byte("{}")
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil && strings.TrimSpace(s) != "" {
			return []byte(s)
		}
	}
	return raw
}

func rpcResult(id any, result any) []byte {
	raw, _ := json.Marshal(result)
	b, _ := json.Marshal(rpcMessage{JSONRPC: "2.0", ID: id, Result: raw})
	return b
}
