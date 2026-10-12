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
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login status %d", res.StatusCode)
	}

	res, err = client.PostForm(ts.URL+"/users", url.Values{
		"name":        {"ann"},
		"forcedModel": {"claude-sonnet"},
		"tokenLimit":  {"100"},
		"resetEvery":  {"week"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, res)
	if !strings.Contains(body, "Токен доступа") {
		t.Fatalf("body %s", body)
	}
	res, err = client.PostForm(ts.URL+"/proxy", url.Values{"proxy": {"http://proxy:3128"}})
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

func readAll(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
