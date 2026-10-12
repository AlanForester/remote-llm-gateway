package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAndCall(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "server.js")
	if err := os.WriteFile(script, []byte(fakeMCP), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "mcp.json")
	raw, _ := json.Marshal(map[string]any{
		"servers": []Server{{Name: "demo", Command: []string{"node", script}}},
	})
	if err := os.WriteFile(cfg, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	servers, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hub, err := Start(ctx, servers)
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	defs := hub.Definitions()
	if len(defs) != 1 || defs[0].Name != "mcp_demo_ping" {
		t.Fatalf("%+v", defs)
	}
	out, err := hub.Call(ctx, "mcp_demo_ping", []byte(`{"text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "pong:hi" {
		t.Fatalf("%q", out)
	}
}

func TestLoadRejectsEmptyCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(`{"servers":[{"name":"x","command":[]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error")
	}
}

const fakeMCP = `
const fs = require("fs");
let buf = Buffer.alloc(0);
function send(obj) {
  const body = Buffer.from(JSON.stringify(obj));
  fs.writeSync(1, Buffer.concat([Buffer.from("Content-Length: " + body.length + "\r\n\r\n"), body]));
}
process.stdin.on("data", (chunk) => {
  buf = Buffer.concat([buf, chunk]);
  for (;;) {
    const sep = buf.indexOf("\r\n\r\n");
    if (sep < 0) return;
    const head = buf.slice(0, sep).toString("utf8");
    const match = head.match(/Content-Length:\s*(\d+)/i);
    if (!match) return;
    const len = Number(match[1]);
    const start = sep + 4;
    if (buf.length < start + len) return;
    const msg = JSON.parse(buf.slice(start, start + len).toString("utf8"));
    buf = buf.slice(start + len);
    if (msg.method === "initialize") {
      send({jsonrpc:"2.0", id: msg.id, result:{protocolVersion:"2024-11-05", capabilities:{tools:{}}, serverInfo:{name:"demo", version:"0"}}});
    } else if (msg.method === "tools/list") {
      send({jsonrpc:"2.0", id: msg.id, result:{tools:[{name:"ping", description:"ping", inputSchema:{type:"object", properties:{text:{type:"string"}}, required:["text"]}}]}});
    } else if (msg.method === "tools/call") {
      const text = (msg.params && msg.params.arguments && msg.params.arguments.text) || "";
      send({jsonrpc:"2.0", id: msg.id, result:{content:[{type:"text", text:"pong:" + text}]}});
    } else if (msg.id !== undefined && msg.method) {
      send({jsonrpc:"2.0", id: msg.id, error:{code:-32601, message:"no"}});
    }
  }
});
`
