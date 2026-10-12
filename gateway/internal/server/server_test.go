package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/AlanForester/remote-llm-gateway/internal/filesync"
	"github.com/AlanForester/remote-llm-gateway/internal/store"
)

func TestForcedModelQuotaAndSocket(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "gateway.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	_, token, err := st.CreateUser("ann", "claude-sonnet", "day", 5, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetProxy("http://admin-proxy:3128", now); err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(dir, ".env")
	if err := os.WriteFile(env, []byte("OUTBOUND_PROXY=http://env-proxy:1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := now.Add(-time.Hour)
	if err := os.Chtimes(env, past, past); err != nil {
		t.Fatal(err)
	}

	clock := now
	srv := New(Options{
		Store:    st,
		Echo:     true,
		WorkRoot: filepath.Join(dir, "work"),
		EnvFile:  env,
		Now:      func() time.Time { return clock },
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := bytes.NewBufferString(`{"workspace":"/local/app","model":"grok"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/sessions", body)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var created struct {
		SessionID string `json:"sessionId"`
		Model     string `json:"model"`
		Forced    bool   `json:"forced"`
	}
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if !created.Forced || created.Model != "claude-sonnet" || created.SessionID == "" {
		t.Fatalf("%+v", created)
	}

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/" + created.SessionID + "?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	send := func(text string) string {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "session/prompt",
			"params":  map[string]string{"text": text},
		})
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			t.Fatal(err)
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		return string(msg)
	}
	if got := send("ab"); !strings.Contains(got, "echo: ab") {
		t.Fatalf("first reply: %s", got)
	}
	if got := send("abcd"); !strings.Contains(got, "echo: abcd") {
		t.Fatalf("second reply: %s", got)
	}
	// 2 + 4 = 6, which is past the limit of 5. The next prompt is refused.
	got := send("x")
	if !strings.Contains(got, "Лимит токенов исчерпан") {
		t.Fatalf("quota reply: %s", got)
	}

	clock = now.Add(25 * time.Hour)
	if got := send("z"); !strings.Contains(got, "echo: z") {
		t.Fatalf("after reset: %s", got)
	}

	me, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	me.Header.Set("Authorization", "Bearer "+token)
	res2, err := http.DefaultClient.Do(me)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	var view store.PublicUser
	if err := json.NewDecoder(res2.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.Model != "claude-sonnet" || !view.Forced || view.TokensUsed != 1 {
		t.Fatalf("%+v", view)
	}
}

func TestSyncFiles(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "gateway.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	_, token, err := st.CreateUser("ann", "", "day", 0, now)
	if err != nil {
		t.Fatal(err)
	}
	_, other, err := st.CreateUser("bob", "", "day", 0, now)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{Store: st, Echo: true, WorkRoot: filepath.Join(dir, "work"), Now: func() time.Time { return now }})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := bytes.NewBufferString(`{"workspace":"/local/app","model":"grok"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/sessions", body)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var created struct {
		SessionID    string `json:"sessionId"`
		WorkspaceDir string `json:"workspaceDir"`
	}
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	put := bytes.NewBufferString(`{"files":[{"path":"main.txt","content":"hello"},{"path":"../etc/passwd","content":"x"}]}`)
	bad, _ := http.NewRequest(http.MethodPut, ts.URL+"/v1/sessions/"+created.SessionID+"/files", put)
	bad.Header.Set("Authorization", "Bearer "+token)
	resBad, err := http.DefaultClient.Do(bad)
	if err != nil {
		t.Fatal(err)
	}
	resBad.Body.Close()
	if resBad.StatusCode != http.StatusBadRequest {
		t.Fatalf("escape status %d", resBad.StatusCode)
	}

	put = bytes.NewBufferString(`{"files":[{"path":"main.txt","content":"hello"},{"path":"old.txt","content":"gone"}]}`)
	okReq, _ := http.NewRequest(http.MethodPut, ts.URL+"/v1/sessions/"+created.SessionID+"/files", put)
	okReq.Header.Set("Authorization", "Bearer "+token)
	resOK, err := http.DefaultClient.Do(okReq)
	if err != nil {
		t.Fatal(err)
	}
	resOK.Body.Close()
	if resOK.StatusCode != http.StatusOK {
		t.Fatalf("put status %d", resOK.StatusCode)
	}

	denied, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/sessions/"+created.SessionID+"/changes", nil)
	denied.Header.Set("Authorization", "Bearer "+other)
	resDenied, err := http.DefaultClient.Do(denied)
	if err != nil {
		t.Fatal(err)
	}
	resDenied.Body.Close()
	if resDenied.StatusCode != http.StatusUnauthorized {
		t.Fatalf("other user %d", resDenied.StatusCode)
	}

	if err := os.WriteFile(filepath.Join(created.WorkspaceDir, "main.txt"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(created.WorkspaceDir, "old.txt")); err != nil {
		t.Fatal(err)
	}
	chg, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/sessions/"+created.SessionID+"/changes", nil)
	chg.Header.Set("Authorization", "Bearer "+token)
	resChg, err := http.DefaultClient.Do(chg)
	if err != nil {
		t.Fatal(err)
	}
	defer resChg.Body.Close()
	var payload struct {
		Files []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
			Deleted bool   `json:"deleted"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resChg.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	got := map[string]struct {
		Content string
		Deleted bool
	}{}
	for _, f := range payload.Files {
		got[f.Path] = struct {
			Content string
			Deleted bool
		}{f.Content, f.Deleted}
	}
	if got["main.txt"].Content != "edited" || got["main.txt"].Deleted {
		t.Fatalf("main: %+v", got["main.txt"])
	}
	if !got["old.txt"].Deleted {
		t.Fatalf("old: %+v", got["old.txt"])
	}
}

func TestCollectAndApplyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "local")
	if err := os.Mkdir(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "note.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "gateway.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	_, token, err := st.CreateUser("ann", "", "day", 0, now)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{Store: st, Echo: true, WorkRoot: filepath.Join(dir, "work"), Now: func() time.Time { return now }})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := bytes.NewBufferString(`{"workspace":"` + local + `","model":""}`)
	create, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/sessions", reqBody)
	create.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(create)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var created struct {
		SessionID    string `json:"sessionId"`
		WorkspaceDir string `json:"workspaceDir"`
	}
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	snap, err := filesync.Collect(local)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"files": snap.Files})
	put, _ := http.NewRequest(http.MethodPut, ts.URL+"/v1/sessions/"+created.SessionID+"/files", bytes.NewReader(raw))
	put.Header.Set("Authorization", "Bearer "+token)
	resPut, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatal(err)
	}
	resPut.Body.Close()
	if resPut.StatusCode != http.StatusOK {
		t.Fatalf("put %d", resPut.StatusCode)
	}
	if err := os.WriteFile(filepath.Join(created.WorkspaceDir, "note.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.WorkspaceDir, "extra.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	chg, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/sessions/"+created.SessionID+"/changes", nil)
	chg.Header.Set("Authorization", "Bearer "+token)
	resChg, err := http.DefaultClient.Do(chg)
	if err != nil {
		t.Fatal(err)
	}
	defer resChg.Body.Close()
	var payload struct {
		Files []filesync.File `json:"files"`
	}
	if err := json.NewDecoder(resChg.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if err := filesync.Apply(local, payload.Files); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(local, "note.txt"))
	if err != nil || string(got) != "two" {
		t.Fatalf("note %q %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(local, "extra.txt"))
	if err != nil || string(got) != "new" {
		t.Fatalf("extra %q %v", got, err)
	}
}

func TestPluginWebSocketClient(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "gateway.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	_, token, err := st.CreateUser("ann", "", "day", 0, now)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{Store: st, Echo: true, WorkRoot: filepath.Join(dir, "work"), Now: func() time.Time { return now }})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := bytes.NewBufferString(`{"workspace":"/tmp/app","model":"grok"}`)
	create, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/sessions", reqBody)
	create.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(create)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var created struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	wsPath, err := filepath.Abs(filepath.Join("..", "..", "..", "plugin", "src", "ws.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
const ws = require(process.env.WS_CLIENT);
(async () => {
  const target = process.env.GATEWAY_HTTP.replace(/^http/, "ws") + "/ws/" + process.env.SESSION_ID + "?token=" + encodeURIComponent(process.env.GATEWAY_TOKEN);
  const client = await ws.connect(target);
  client.send(JSON.stringify({ jsonrpc: "2.0", id: 1, method: "session/prompt", params: { text: "hi" } }));
  const text = await client.next();
  if (!text.includes("echo: hi")) {
    console.error(text);
    process.exit(1);
  }
  client.close();
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
`
	cmd := exec.Command("node", "-e", script)
	cmd.Env = append(os.Environ(),
		"WS_CLIENT="+wsPath,
		"GATEWAY_HTTP="+ts.URL,
		"GATEWAY_TOKEN="+token,
		"SESSION_ID="+created.SessionID,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
