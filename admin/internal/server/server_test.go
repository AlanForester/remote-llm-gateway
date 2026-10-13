package server

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlanForester/remote-llm-gateway/internal/store"
)

func TestAdminCreatesUserAndProxy(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "gateway.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	srv := New(Options{Store: st, AdminToken: "secret", Now: func() time.Time { return now }})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := ts.Client()
	client.Jar = jar

	res, err := client.PostForm(ts.URL+"/login", url.Values{"token": {"secret"}})
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login status %d", res.StatusCode)
	}
	csrf := csrfFrom(t, body)

	denied, err := client.PostForm(ts.URL+"/users", url.Values{"name": {"ann"}})
	if err != nil {
		t.Fatal(err)
	}
	denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("missing csrf %d", denied.StatusCode)
	}

	res, err = client.PostForm(ts.URL+"/users", url.Values{
		"csrf":        {csrf},
		"name":        {"ann"},
		"forcedModel": {"claude-sonnet"},
		"tokenLimit":  {"100"},
		"resetEvery":  {"week"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body = readAll(t, res)
	if !strings.Contains(body, "Токен доступа") {
		t.Fatalf("body %s", body)
	}
	if !strings.Contains(body, `<select name="forcedModel">`) || !strings.Contains(body, `<select name="resetEvery">`) {
		t.Fatal("model and period selectors missing")
	}
	if !strings.Contains(body, `value="claude-sonnet" selected`) || !strings.Contains(body, `value="week" selected`) {
		t.Fatal("saved model or period is not selected")
	}
	if !strings.Contains(body, "Подключение моделей") || !strings.Contains(body, ">Grok<") {
		t.Fatal("provider status missing")
	}
	if res.Request.URL.RawQuery != "" {
		t.Fatalf("token leaked into the url: %s", res.Request.URL)
	}
	again, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readAll(t, again), "Токен доступа") {
		t.Fatal("access token shown twice")
	}
	badProxy, err := client.PostForm(ts.URL+"/proxy", url.Values{"csrf": {csrf}, "proxy": {"http://ok\nEVIL=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readAll(t, badProxy), "socks5") {
		t.Fatal("invalid proxy was accepted")
	}
	if got, _, err := st.ProxySettings(); err != nil || got != "" {
		t.Fatalf("proxy stored early: %q %v", got, err)
	}
	res, err = client.PostForm(ts.URL+"/proxy", url.Values{"csrf": {csrf}, "proxy": {"http://proxy:3128"}})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	users, err := st.List(now)
	if err != nil || len(users) != 1 || users[0].ForcedModel != "claude-sonnet" || users[0].TokenLimit != 100 {
		t.Fatalf("%+v %v", users, err)
	}
	got, _, err := st.ProxySettings()
	if err != nil || got != "http://proxy:3128" {
		t.Fatalf("proxy %q %v", got, err)
	}
}

func csrfFrom(t *testing.T, body string) string {
	t.Helper()
	const needle = `name="csrf" value="`
	i := strings.Index(body, needle)
	if i < 0 {
		t.Fatalf("csrf field missing: %s", body)
	}
	rest := body[i+len(needle):]
	j := strings.Index(rest, `"`)
	if j <= 0 {
		t.Fatal("csrf value missing")
	}
	return rest[:j]
}

func readAll(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
