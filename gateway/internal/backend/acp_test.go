package backend

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreferAllow(t *testing.T) {
	got := preferAllow([]permOption{
		{Kind: "reject_once", OptionID: "no"},
		{Kind: "allow_once", OptionID: "once"},
		{Kind: "allow_always", OptionID: "always"},
	})
	if got != "always" {
		t.Fatal(got)
	}
	if got := preferAllow([]permOption{{Kind: "allow_once", OptionID: "once"}}); got != "once" {
		t.Fatal(got)
	}
	if got := preferAllow(nil); got != "" {
		t.Fatal(got)
	}
}

func TestACPStreamsThoughtAndAnswer(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	seen := filepath.Join(dir, "seen.json")
	script := filepath.Join(dir, "agent.js")
	if err := os.WriteFile(script, []byte(fakeACP), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACP_SEEN", seen)
	agent, err := startProcess(context.Background(), []string{"node", script}, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()

	rawSeen, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	var opened struct {
		Cwd  string `json:"cwd"`
		Yolo bool   `json:"yolo"`
	}
	if err := json.Unmarshal(rawSeen, &opened); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Cwd != abs || opened.Yolo {
		t.Fatalf("session %+v", opened)
	}

	if err := agent.Send(context.Background(), []byte(`{"jsonrpc":"2.0","id":7,"method":"session/prompt","params":{"text":"привет"}}`)); err != nil {
		t.Fatal(err)
	}
	thought, err := agent.Recv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(thought), "agent_thought_chunk") || !strings.Contains(string(thought), "думаю привет") {
		t.Fatalf("thought %s", thought)
	}
	chunk, err := agent.Recv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chunk), "agent_message_chunk") || !strings.Contains(string(chunk), "готово") {
		t.Fatalf("chunk %s", chunk)
	}
	final, err := agent.Recv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(final), `"id":7`) || !strings.Contains(string(final), "готово") || !strings.Contains(string(final), "end_turn") {
		t.Fatalf("final %s", final)
	}
}

func TestACPEffortAndManualPermission(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	seen := filepath.Join(dir, "seen.json")
	effortPath := filepath.Join(dir, "effort.json")
	script := filepath.Join(dir, "agent.js")
	if err := os.WriteFile(script, []byte(fakeACP), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACP_SEEN", seen)
	t.Setenv("ACP_EFFORT", effortPath)
	agent, err := startProcess(context.Background(), []string{"node", script}, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	payload := `{"jsonrpc":"2.0","id":7,"method":"session/prompt","params":{"text":"привет","effort":"high","autoAccept":false}}`
	if err := agent.Send(context.Background(), []byte(payload)); err != nil {
		t.Fatal(err)
	}
	thought, err := agent.Recv(context.Background())
	if err != nil || !strings.Contains(string(thought), "agent_thought_chunk") {
		t.Fatalf("thought %s %v", thought, err)
	}
	rawEffort, err := os.ReadFile(effortPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawEffort), `"reasoning_effort"`) || !strings.Contains(string(rawEffort), `"high"`) {
		t.Fatalf("effort %s", rawEffort)
	}
	perm, err := agent.Recv(context.Background())
	if err != nil || !strings.Contains(string(perm), "session/request_permission") {
		t.Fatalf("permission %s %v", perm, err)
	}
	allow := []byte(`{"jsonrpc":"2.0","id":9,"result":{"outcome":{"outcome":"selected","optionId":"once"}}}`)
	if err := agent.Send(context.Background(), allow); err != nil {
		t.Fatal(err)
	}
	chunk, err := agent.Recv(context.Background())
	if err != nil || !strings.Contains(string(chunk), "готово") {
		t.Fatalf("chunk %s %v", chunk, err)
	}
}

const fakeACP = `
const fs = require("fs");
const readline = require("readline");
const rl = readline.createInterface({ input: process.stdin, terminal: false });
let promptId = null;
function send(obj) {
  fs.writeSync(1, JSON.stringify(obj) + "\n");
}
rl.on("line", (line) => {
  const msg = JSON.parse(line);
  if (msg.method === "initialize") {
    send({ jsonrpc: "2.0", id: msg.id, result: { protocolVersion: 1 } });
    return;
  }
  if (msg.method === "session/set_config_option") {
    if (process.env.ACP_EFFORT) fs.writeFileSync(process.env.ACP_EFFORT, JSON.stringify(msg.params));
    send({ jsonrpc: "2.0", id: msg.id, result: { configOptions: [] } });
    return;
  }
  if (msg.method === "session/new") {
    fs.writeFileSync(process.env.ACP_SEEN, JSON.stringify({
      cwd: msg.params.cwd,
      mcp: msg.params.mcpServers,
      yolo: !!(msg.params._meta && msg.params._meta.yoloMode),
    }));
    send({ jsonrpc: "2.0", id: msg.id, result: { sessionId: "sess-1" } });
    return;
  }
  if (msg.method === "session/prompt") {
    promptId = msg.id;
    const text = (msg.params.prompt || []).map((block) => block.text || "").join("");
    if (msg.params.sessionId !== "sess-1") process.exit(3);
    send({
      jsonrpc: "2.0",
      method: "session/update",
      params: {
        sessionId: "sess-1",
        update: { sessionUpdate: "agent_thought_chunk", content: { type: "text", text: "думаю " + text } },
      },
    });
    send({
      jsonrpc: "2.0",
      id: 9,
      method: "session/request_permission",
      params: {
        sessionId: "sess-1",
        options: [
          { optionId: "no", kind: "reject_once", name: "No" },
          { optionId: "once", kind: "allow_once", name: "Once" },
        ],
        toolCall: { toolCallId: "call-1", title: "read file" },
      },
    });
    return;
  }
  if (msg.id === 9) {
    const option = msg.result && msg.result.outcome && msg.result.outcome.optionId;
    if (option !== "once") process.exit(4);
    send({
      jsonrpc: "2.0",
      method: "session/update",
      params: {
        sessionId: "sess-1",
        update: { sessionUpdate: "agent_message_chunk", content: { type: "text", text: "готово" } },
      },
    });
    send({ jsonrpc: "2.0", id: promptId, result: { stopReason: "end_turn" } });
  }
});
`
