// Package server is the small web admin for users, models, limits, and the proxy.
package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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
	s.tpl = template.Must(template.New("admin").Parse(page))
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
	rows := make([]userRow, 0, len(users))
	for _, u := range users {
		p := store.Public(u, "", now)
		rows = append(rows, userRow{User: u, ResetAt: p.ResetAt.Format(time.RFC3339)})
	}
	adminURL, adminAt, _ := s.opt.Store.ProxySettings()
	usage, _ := s.opt.Store.Usage(20)
	return view{
		Users:      rows,
		Usage:      usage,
		Proxy:      adminURL,
		Effective:  proxy.Resolve(adminURL, adminAt, s.opt.EnvFile),
		FreshToken: freshToken,
		Error:      errText,
		CSRF:       csrf,
	}
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
	Login      bool
	Error      string
	Users      []userRow
	Usage      []store.UsageEvent
	Proxy      string
	Effective  string
	FreshToken string
	CSRF       string
}

type userRow struct {
	store.User
	ResetAt string
}

const page = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<title>Remote LLM Gateway</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; margin: 2rem; color: #1a1a1a; }
table { border-collapse: collapse; width: 100%; margin: 1rem 0; }
td, th { border-bottom: 1px solid #ddd; padding: 0.4rem; text-align: left; vertical-align: top; }
input, button { font: inherit; padding: 0.3rem 0.5rem; }
.err { color: #8a1f1f; }
.token { background: #f4f1e8; padding: 0.6rem; }
form.inline { display: inline; }
</style>
</head>
<body>
{{if .Login}}
<h1>Вход в админку</h1>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<form method="post" action="/login">
<label>Токен администратора <input name="token" type="password" required></label>
<button>Войти</button>
</form>
{{else}}
<h1>Админка</h1>
<form method="post" action="/logout"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>Выйти</button></form>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
{{if .FreshToken}}<p class="token">Токен доступа, показывается один раз: <code>{{.FreshToken}}</code></p>{{end}}

<h2>Исходящий прокси</h2>
<p>Сейчас действует: <code>{{if .Effective}}{{.Effective}}{{else}}не задан{{end}}</code></p>
<form method="post" action="/proxy">
<input type="hidden" name="csrf" value="{{.CSRF}}">
<label>Адрес в админке <input name="proxy" value="{{.Proxy}}" placeholder="http://proxy:3128"></label>
<button>Сохранить</button>
</form>
<p>Пустое поле оставляет прокси из <code>OUTBOUND_PROXY</code> в <code>.env</code>. Более позднее изменение побеждает.</p>

<h2>Новый пользователь</h2>
<form method="post" action="/users">
<input type="hidden" name="csrf" value="{{.CSRF}}">
<label>Имя <input name="name" required></label>
<label>Принудительная LLM <input name="forcedModel" placeholder="claude-sonnet"></label>
<label>Лимит токенов <input name="tokenLimit" value="0"></label>
<label>Период сброса <input name="resetEvery" value="day" placeholder="day, week, month или 12h"></label>
<button>Создать</button>
</form>

<h2>Пользователи</h2>
<table>
<tr><th>Имя</th><th>Модель</th><th>Токены</th><th>Период</th><th>Сброс</th><th></th></tr>
{{range .Users}}
<tr>
<td colspan="6">
<form method="post" action="/users/update">
<input type="hidden" name="csrf" value="{{$.CSRF}}">
<input type="hidden" name="id" value="{{.ID}}">
<input name="name" value="{{.Name}}">
<input name="forcedModel" value="{{.ForcedModel}}" placeholder="модель">
<input name="tokenLimit" value="{{.TokenLimit}}">
<input name="resetEvery" value="{{.ResetEvery}}">
<span>{{.TokensUsed}} использовано, сброс {{.ResetAt}}</span>
<button>Сохранить</button>
</form>
<form class="inline" method="post" action="/users/delete">
<input type="hidden" name="csrf" value="{{$.CSRF}}">
<input type="hidden" name="id" value="{{.ID}}">
<button>Удалить</button>
</form>
</td>
</tr>
{{end}}
</table>

<h2>Последний расход</h2>
<table>
<tr><th>Когда</th><th>Пользователь</th><th>Модель</th><th>Токены</th></tr>
{{range .Usage}}
<tr><td>{{.At.Format "2006-01-02 15:04"}}</td><td>{{.UserID}}</td><td>{{.Model}}</td><td>{{.Tokens}}</td></tr>
{{end}}
</table>
{{end}}
</body>
</html>
`
