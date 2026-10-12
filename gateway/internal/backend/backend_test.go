package backend

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEchoCountsRunes(t *testing.T) {
	agent := startEcho()
	defer agent.Close()
	msg := []byte(`{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"text":"аб"}}`)
	if err := agent.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	got, err := agent.Recv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Result struct {
			Text  string `json:"text"`
			Usage struct {
				TotalTokens int `json:"totalTokens"`
			} `json:"usage"`
		} `json:"result"`
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if out.Result.Text != "echo: аб" || out.Result.Usage.TotalTokens != 2 {
		t.Fatalf("%s", got)
	}
}

func TestOpenAIUsesProxyAndUsage(t *testing.T) {
	var gotProxy string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth %s", r.Header.Get("Authorization"))
		}
		_, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": "привет",
			"usage":       map[string]int{"total_tokens": 4},
		})
	}))
	defer upstream.Close()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProxy = r.URL.String()
		// Forward the absolute-form request the client sent through the proxy.
		req, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		req.Header = r.Header.Clone()
		res, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		defer res.Body.Close()
		for k, vs := range res.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
	}))
	defer proxy.Close()

	agent, err := startOpenAI(upstream.URL, "test-key", "gpt-4o", proxy.URL, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	if err := agent.Send(context.Background(), []byte(`{"jsonrpc":"2.0","id":7,"method":"session/prompt","params":{"text":"hi"}}`)); err != nil {
		t.Fatal(err)
	}
	raw, err := agent.Recv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "привет") || !strings.Contains(string(raw), `"totalTokens":4`) {
		t.Fatalf("%s", raw)
	}
	if gotProxy == "" {
		t.Fatal("request did not pass the proxy")
	}
}

func TestOpenAIWritesFileThroughTool(t *testing.T) {
	dir := t.TempDir()
	var round int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
			Previous string `json:"previous_response_id"`
			Input    []struct {
				Type   string `json:"type"`
				Output string `json:"output"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode %v", err)
		}
		round++
		if round == 1 {
			names := map[string]bool{}
			for _, tool := range body.Tools {
				names[tool.Name] = true
			}
			if !names["write_file"] || !names["read_file"] || !names["run_command"] {
				t.Errorf("tools %+v", body.Tools)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "resp_1",
				"output": []any{
					map[string]any{
						"type":      "function_call",
						"call_id":   "call_1",
						"name":      "write_file",
						"arguments": `{"path":"note.txt","content":"hello"}`,
					},
				},
				"usage": map[string]int{"total_tokens": 2},
			})
			return
		}
		if body.Previous != "resp_1" || len(body.Input) != 1 || !strings.Contains(body.Input[0].Output, "записан note.txt") {
			t.Errorf("follow-up %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "resp_2",
			"output_text": "готово",
			"usage":       map[string]int{"total_tokens": 3},
		})
	}))
	defer upstream.Close()

	agent, err := startOpenAI(upstream.URL, "test-key", "gpt-4o", "", dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	if err := agent.Send(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"text":"создай файл"}}`)); err != nil {
		t.Fatal(err)
	}
	raw, err := agent.Recv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "готово") || !strings.Contains(string(raw), `"totalTokens":5`) {
		t.Fatalf("%s", raw)
	}
	got, err := os.ReadFile(filepath.Join(dir, "note.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestReadRejectsEscapeAndHidesSecrets(t *testing.T) {
	dir := t.TempDir()
	if got := runTool(context.Background(), dir, nil, "read_file", []byte(`{"path":"../secret"}`)); !strings.Contains(got, "запрещ") {
		t.Fatalf("%s", got)
	}
	t.Setenv("OPENAI_API_KEY", "sk-hidden")
	out, err := toolRun(context.Background(), dir, []byte(`{"command":"printenv OPENAI_API_KEY || true"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "sk-hidden") {
		t.Fatalf("%s", out)
	}
}

func TestKind(t *testing.T) {
	if kindOf("Claude Sonnet") != "claude" || kindOf("gpt-4o") != "gpt" || kindOf("codex") != "gpt" || kindOf("grok-4") != "grok" {
		t.Fatal(kindOf("Claude Sonnet"), kindOf("gpt-4o"), kindOf("codex"), kindOf("grok-4"))
	}
}
