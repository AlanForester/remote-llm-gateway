// Package server is the small web admin for users, models, limits, and the proxy.
package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AlanForester/remote-llm-gateway/internal/metrics"
	"github.com/AlanForester/remote-llm-gateway/internal/proxy"
	"github.com/AlanForester/remote-llm-gateway/internal/store"
)

//go:embed page.html
var pageHTML string

const (
	cookieName  = "admin_session"
	freshCookie = "admin_fresh"
	csrfCookie  = "admin_csrf"
)

// Options configures the admin server.
type Options struct {
	Store      *store.Store
	AdminToken string
	EnvFile    string
	Now        func() time.Time
}

// Server serves the admin UI.
type Server struct {
	opt Options
	mux *http.ServeMux
	tpl *template.Template
}

// New builds the admin handler.
func New(opt Options) *Server {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	s := &Server{opt: opt, mux: http.NewServeMux()}
	s.tpl = template.Must(template.New("admin").Parse(pageHTML))
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	s.mux.Handle("GET /metrics", metrics.Handler())
	s.mux.HandleFunc("GET /login", s.loginPage)
	s.mux.HandleFunc("POST /login", s.login)
	s.mux.HandleFunc("POST /logout", s.logout)
	s.mux.HandleFunc("GET /", s.home)
	s.mux.HandleFunc("POST /users", s.createUser)
	s.mux.HandleFunc("POST /users/update", s.updateUser)
	s.mux.HandleFunc("POST /users/delete", s.deleteUser)
	s.mux.HandleFunc("POST /proxy", s.setProxy)
	return s
}

// Handler is the HTTP handler.
func (s *Server) Handler() http.Handler { return metrics.Instrument("admin", s.mux) }

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, view{Login: true})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if err := s.parseForm(w, r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !sameSecret(r.FormValue("token"), s.opt.AdminToken) {
		s.render(w, view{Login: true, Error: "Неверный токен администратора"})
		return
	}
	s.setCookie(w, r, cookieName, tokenCookie(s.opt.AdminToken), 0)
	s.ensureCSRF(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.parseForm(w, r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !s.auth(r) || !s.csrfOK(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.setCookie(w, r, cookieName, "", -1)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	if !s.auth(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.render(w, s.pageView(s.takeFresh(w, r), "", s.ensureCSRF(w, r)))
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	limit, err := strconv.ParseInt(defaultString(r.FormValue("tokenLimit"), "0"), 10, 64)
	if err != nil {
		s.fail(w, r, "Лимит токенов должен быть числом")
		return
	}
	_, token, err := s.opt.Store.CreateUser(r.FormValue("name"), r.FormValue("forcedModel"), r.FormValue("resetEvery"), limit, s.opt.Now())
	if err != nil {
		s.fail(w, r, userErr(err))
		return
	}
	s.setCookie(w, r, freshCookie, token, 120)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	limit, err := strconv.ParseInt(defaultString(r.FormValue("tokenLimit"), "0"), 10, 64)
	if err != nil {
		s.fail(w, r, "Лимит токенов должен быть числом")
		return
	}
	name := r.FormValue("name")
	forced := r.FormValue("forcedModel")
	every := r.FormValue("resetEvery")
	_, err = s.opt.Store.UpdateUser(r.FormValue("id"), store.UserPatch{
		Name:        &name,
		ForcedModel: &forced,
		TokenLimit:  &limit,
		ResetEvery:  &every,
	}, s.opt.Now())
	if err != nil {
		s.fail(w, r, userErr(err))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	if err := s.opt.Store.DeleteUser(r.FormValue("id")); err != nil {
		s.fail(w, r, userErr(err))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) setProxy(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	raw := strings.TrimSpace(r.FormValue("proxy"))
	if !proxy.Valid(raw) {
		s.fail(w, r, "Адрес прокси должен быть http, https или socks5")
		return
	}
	if err := s.opt.Store.SetProxy(raw, s.opt.Now()); err != nil {
		s.fail(w, r, userErr(err))
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) require(w http.ResponseWriter, r *http.Request) bool {
	if err := s.parseForm(w, r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return false
	}
	if !s.auth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	if !s.csrfOK(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, text string) {
	s.render(w, s.pageView("", text, s.csrfValue(r)))
}

func (s *Server) auth(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil || s.opt.AdminToken == "" {
		return false
	}
	want := tokenCookie(s.opt.AdminToken)
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(want)) == 1
}

func (s *Server) pageView(freshToken, errText, csrf string) view {
	now := s.opt.Now()
	users, _ := s.opt.Store.List(now)
	names := map[string]string{}
	rows := make([]userRow, 0, len(users))
	var tokensPeriod int64
	var overLimit int
	for _, u := range users {
		p := store.Public(u, "", now)
		names[u.ID] = u.Name
		tokensPeriod += p.TokensUsed
		if p.TokenLimit > 0 && p.TokensUsed >= p.TokenLimit {
			overLimit++
		}
		shown := u
		shown.TokensUsed = p.TokensUsed
		rows = append(rows, userRow{
			User:          shown,
			ResetAt:       p.ResetAt.UTC().Format("02.01.2006 15:04 UTC"),
			ModelOptions:  modelOptions(u.ForcedModel),
			PeriodOptions: periodOptions(u.ResetEvery),
		})
	}
	adminURL, adminAt, _ := s.opt.Store.ProxySettings()
	usage, _ := s.opt.Store.Usage(1000)
	return view{
		Users:         rows,
		UsageRows:     usageRows(usage, names),
		ModelBars:     modelBars(usage),
		Stats:         dashStats{Users: len(users), TokensPeriod: tokensPeriod, Events: len(usage), OverLimit: overLimit},
		Providers:     probeProviders(),
		ModelOptions:  modelOptions(""),
		PeriodOptions: periodOptions("day"),
		Proxy:         adminURL,
		Effective:     proxy.Resolve(adminURL, adminAt, s.opt.EnvFile),
		FreshToken:    freshToken,
		Error:         errText,
		CSRF:          csrf,
	}
}

func usageRows(events []store.UsageEvent, names map[string]string) []usageRow {
	rows := make([]usageRow, 0, len(events))
	for i := len(events) - 1; i >= 0 && len(rows) < 15; i-- {
		ev := events[i]
		name := names[ev.UserID]
		if name == "" {
			name = ev.UserID
		}
		model := ev.Model
		if model == "" {
			model = "grok"
		}
		rows = append(rows, usageRow{
			When:   ev.At.UTC().Format("02.01.2006 15:04"),
			User:   name,
			Model:  model,
			Tokens: ev.Tokens,
		})
	}
	return rows
}

func modelBars(events []store.UsageEvent) []modelBar {
	order := []string{}
	byName := map[string]*modelBar{}
	var max int64
	for _, ev := range events {
		name := ev.Model
		if name == "" {
			name = "grok"
		}
		bar := byName[name]
		if bar == nil {
			bar = &modelBar{Name: name}
			byName[name] = bar
			order = append(order, name)
		}
		bar.Tokens += ev.Tokens
		bar.Calls++
		if bar.Tokens > max {
			max = bar.Tokens
		}
	}
	out := make([]modelBar, 0, len(order))
	for _, name := range order {
		bar := *byName[name]
		if max > 0 {
			bar.Width = int(bar.Tokens * 100 / max)
			if bar.Width < 2 && bar.Tokens > 0 {
				bar.Width = 2
			}
		}
		out = append(out, bar)
	}
	return out
}

func (s *Server) render(w http.ResponseWriter, v view) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	if err := s.tpl.Execute(w, v); err != nil {
		http.Error(w, "template", http.StatusInternalServerError)
	}
}

func (s *Server) parseForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	return r.ParseForm()
}

func (s *Server) setCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secureRequest(r),
	}
	if maxAge != 0 {
		c.MaxAge = maxAge
	}
	http.SetCookie(w, c)
}

func (s *Server) ensureCSRF(w http.ResponseWriter, r *http.Request) string {
	if value := s.csrfValue(r); value != "" {
		return value
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	value := hex.EncodeToString(buf)
	s.setCookie(w, r, csrfCookie, value, 0)
	return value
}

func (s *Server) csrfValue(r *http.Request) string {
	c, err := r.Cookie(csrfCookie)
	if err != nil || len(c.Value) != 64 {
		return ""
	}
	return c.Value
}

func (s *Server) csrfOK(r *http.Request) bool {
	want := s.csrfValue(r)
	got := r.FormValue("csrf")
	if want == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (s *Server) takeFresh(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(freshCookie)
	if err != nil || c.Value == "" {
		return ""
	}
	s.setCookie(w, r, freshCookie, "", -1)
	return c.Value
}

func secureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func sameSecret(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	sumGot := sha256.Sum256([]byte(got))
	sumWant := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(sumGot[:], sumWant[:]) == 1
}

func userErr(err error) string {
	if errors.Is(err, store.ErrBadInput) {
		return "Проверьте имя, модель, лимит и период"
	}
	return err.Error()
}

func tokenCookie(token string) string {
	sum := sha256.Sum256([]byte("admin:" + token))
	return hex.EncodeToString(sum[:])
}

func defaultString(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

type view struct {
	Login         bool
	Error         string
	Users         []userRow
	UsageRows     []usageRow
	ModelBars     []modelBar
	Stats         dashStats
	Providers     []providerCard
	ModelOptions  []option
	PeriodOptions []option
	Proxy         string
	Effective     string
	FreshToken    string
	CSRF          string
}

type dashStats struct {
	Users        int
	TokensPeriod int64
	Events       int
	OverLimit    int
}

type usageRow struct {
	When   string
	User   string
	Model  string
	Tokens int64
}

type modelBar struct {
	Name   string
	Tokens int64
	Calls  int
	Width  int
}

type userRow struct {
	store.User
	ResetAt       string
	ModelOptions  []option
	PeriodOptions []option
}
