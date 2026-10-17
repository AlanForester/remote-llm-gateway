package backend

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// acpAgent speaks Agent Client Protocol to a grok or claude process.
// The server directory stays on the gateway: the client still sends {text}.
type acpAgent struct {
	raw        Agent
	sessionID  string
	mu         sync.Mutex
	next       int
	active     string
	autoAccept bool
	clientIDs  map[string]json.RawMessage
	texts      map[string]*strings.Builder
	silent     map[string]bool
}

func (a *acpAgent) handshake(ctx context.Context, dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	hctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := a.raw.Send(hctx, rpcCall(1, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientInfo": map[string]string{
			"name":    "remote-llm-gateway",
			"version": "0.1.0",
		},
		"clientCapabilities": map[string]any{
			"fs":       map[string]bool{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
	})); err != nil {
		return "", err
	}
	if _, err := a.waitID(hctx, 1); err != nil {
		return "", err
	}
	if err := a.raw.Send(hctx, rpcCall(2, "session/new", map[string]any{
		"cwd":        abs,
		"mcpServers": []any{},
	})); err != nil {
		return "", err
	}
	raw, err := a.waitID(hctx, 2)
	if err != nil {
		return "", err
	}
	var created struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || created.SessionID == "" {
		return "", errors.New("агент не открыл сессию")
	}
	return created.SessionID, nil
}

func (a *acpAgent) Send(ctx context.Context, msg []byte) error {
	if isClientResult(msg) {
		return a.raw.Send(ctx, msg)
	}
	var in rpcMessage
	if err := json.Unmarshal(msg, &in); err != nil {
		return err
	}
	if in.Method == "session/cancel" {
		body, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"method":  "session/cancel",
			"params":  map[string]string{"sessionId": a.sessionID},
		})
		if err != nil {
			return err
		}
		return a.raw.Send(ctx, body)
	}
	if !isPromptMethod(in.Method) {
		return a.raw.Send(ctx, msg)
	}
	text := promptText(in.Params)
	effort, accept := promptOptions(in.Params)
	if accept != nil {
		a.mu.Lock()
		a.autoAccept = *accept
		a.mu.Unlock()
	}
	if err := a.sendEffort(ctx, effort); err != nil {
		return err
	}
	idRaw, err := json.Marshal(in.ID)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.next++
	key := strconv.Itoa(a.next)
	a.clientIDs[key] = idRaw
	a.texts[key] = &strings.Builder{}
	a.active = key
	promptID := a.next
	a.mu.Unlock()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      promptID,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": a.sessionID,
			"prompt":    []any{map[string]string{"type": "text", "text": text}},
		},
	})
	if err != nil {
		return err
	}
	return a.raw.Send(ctx, body)
}

func (a *acpAgent) Recv(ctx context.Context) ([]byte, error) {
	for {
		msg, err := a.nextMessage(ctx)
		if err != nil {
			return nil, err
		}
		method, id := headOf(msg)
		if a.dropSilent(id) {
			continue
		}
		if method != "" {
			return msg, nil
		}
		clientID, text, ok := a.take(id)
		if !ok {
			return msg, nil
		}
		return rewriteResult(msg, clientID, text), nil
	}
}

func (a *acpAgent) Close() error { return a.raw.Close() }

func (a *acpAgent) waitID(ctx context.Context, id int) (json.RawMessage, error) {
	want := strconv.Itoa(id)
	for {
		msg, err := a.nextMessage(ctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, errors.New("агент не ответил")
			}
			return nil, err
		}
		method, got := headOf(msg)
		if method != "" || got != want {
			continue
		}
		var body struct {
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if err := json.Unmarshal(msg, &body); err != nil {
			return nil, err
		}
		if body.Error != nil {
			if body.Error.Message == "" {
				return nil, errors.New("агент отказал")
			}
			return nil, errors.New(body.Error.Message)
		}
		return body.Result, nil
	}
}

// nextMessage forwards nothing. It answers requests the agent sends back,
// such as permission prompts, so a turn cannot stall waiting for the UI.
func (a *acpAgent) nextMessage(ctx context.Context) ([]byte, error) {
	for {
		msg, err := a.raw.Recv(ctx)
		if err != nil {
			return nil, err
		}
		method, id := headOf(msg)
		if method == "session/update" {
			a.noteUpdate(msg)
			return msg, nil
		}
		if method == "session/request_permission" && id != "" && !a.accepting() {
			return msg, nil
		}
		if method != "" && id != "" {
			if err := a.answer(ctx, method, msg); err != nil {
				return nil, err
			}
			continue
		}
		return msg, nil
	}
}

func (a *acpAgent) accepting() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.autoAccept
}

func (a *acpAgent) dropSilent(id string) bool {
	if id == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.silent[id] {
		return false
	}
	delete(a.silent, id)
	return true
}

func (a *acpAgent) sendEffort(ctx context.Context, effort string) error {
	effort = normalizeEffort(effort)
	if effort == "" {
		return nil
	}
	a.mu.Lock()
	a.next++
	id := a.next
	a.silent[strconv.Itoa(id)] = true
	a.mu.Unlock()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/set_config_option",
		"params": map[string]any{
			"sessionId": a.sessionID,
			"configId":  "reasoning_effort",
			"value":     map[string]string{"value": effort},
		},
	})
	if err != nil {
		return err
	}
	return a.raw.Send(ctx, body)
}

func (a *acpAgent) answer(ctx context.Context, method string, msg []byte) error {
	if method == "session/request_permission" {
		return a.raw.Send(ctx, allowPermission(msg))
	}
	var head struct {
		ID json.RawMessage `json:"id"`
	}
	_ = json.Unmarshal(msg, &head)
	return a.raw.Send(ctx, rpcFail(head.ID, -32601, method+" недоступен"))
}

func (a *acpAgent) noteUpdate(msg []byte) {
	text := messageChunk(msg)
	if text == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if b := a.texts[a.active]; b != nil {
		b.WriteString(text)
	}
}

func (a *acpAgent) take(id string) (json.RawMessage, string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	raw, ok := a.clientIDs[id]
	if !ok {
		return nil, "", false
	}
	text := ""
	if b := a.texts[id]; b != nil {
		text = b.String()
	}
	delete(a.clientIDs, id)
	delete(a.texts, id)
	if a.active == id {
		a.active = ""
	}
	return raw, text, true
}

func rpcCall(id int, method string, params any) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	return b
}

func isClientResult(msg []byte) bool {
	var head struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(msg, &head); err != nil {
		return false
	}
	if head.Method != "" || idKey(head.ID) == "" {
		return false
	}
	return len(head.Result) > 0 || len(head.Error) > 0
}

func promptOptions(params json.RawMessage) (string, *bool) {
	var body struct {
		Effort     string `json:"effort"`
		AutoAccept *bool  `json:"autoAccept"`
	}
	_ = json.Unmarshal(params, &body)
	return body.Effort, body.AutoAccept
}

func normalizeEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "medium", "high", "xhigh":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func promptText(params json.RawMessage) string {
	var body struct {
		Text   string `json:"text"`
		Prompt []struct {
			Text string `json:"text"`
		} `json:"prompt"`
	}
	_ = json.Unmarshal(params, &body)
	if len(body.Prompt) > 0 {
		var b strings.Builder
		for _, block := range body.Prompt {
			b.WriteString(block.Text)
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return body.Text
}

func headOf(msg []byte) (method, id string) {
	var head struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	_ = json.Unmarshal(msg, &head)
	return head.Method, idKey(head.ID)
}

func idKey(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	return s
}

func messageChunk(msg []byte) string {
	var body struct {
		Params struct {
			Update struct {
				SessionUpdate string          `json:"sessionUpdate"`
				Content       json.RawMessage `json:"content"`
			} `json:"update"`
		} `json:"params"`
	}
	if err := json.Unmarshal(msg, &body); err != nil {
		return ""
	}
	if body.Params.Update.SessionUpdate != "agent_message_chunk" {
		return ""
	}
	return contentText(body.Params.Update.Content)
}

func contentText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var b strings.Builder
		for _, block := range blocks {
			b.WriteString(block.Text)
		}
		return b.String()
	}
	var block struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &block)
	return block.Text
}

type permOption struct {
	Kind     string `json:"kind"`
	OptionID string `json:"optionId"`
}

func preferAllow(options []permOption) string {
	once := ""
	for _, opt := range options {
		if opt.OptionID == "" {
			continue
		}
		if opt.Kind == "allow_always" {
			return opt.OptionID
		}
		if opt.Kind == "allow_once" && once == "" {
			once = opt.OptionID
		}
	}
	if once != "" {
		return once
	}
	for _, opt := range options {
		if opt.OptionID != "" {
			return opt.OptionID
		}
	}
	return ""
}

func allowPermission(msg []byte) []byte {
	var in struct {
		ID     json.RawMessage `json:"id"`
		Params struct {
			Options []permOption `json:"options"`
		} `json:"params"`
	}
	_ = json.Unmarshal(msg, &in)
	option := preferAllow(in.Params.Options)
	var outcome any
	if option == "" {
		outcome = map[string]string{"outcome": "cancelled"}
	} else {
		outcome = map[string]string{"outcome": "selected", "optionId": option}
	}
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      in.ID,
		"result":  map[string]any{"outcome": outcome},
	})
	return b
}

func rewriteResult(msg, clientID []byte, text string) []byte {
	var body struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	_ = json.Unmarshal(msg, &body)
	out := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(clientID),
	}
	if len(body.Error) > 0 && string(body.Error) != "null" {
		out["error"] = body.Error
		b, _ := json.Marshal(out)
		return b
	}
	result := map[string]any{}
	if len(body.Result) > 0 && string(body.Result) != "null" {
		_ = json.Unmarshal(body.Result, &result)
	}
	if text != "" {
		existing, _ := result["text"].(string)
		if existing == "" {
			result["text"] = text
		}
	}
	out["result"] = result
	b, _ := json.Marshal(out)
	return b
}
