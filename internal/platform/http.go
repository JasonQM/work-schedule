package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JasonQM/work-schedule/skills"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type contextKey int

const userKey contextKey = 1

func actor(ctx context.Context) User { u, _ := ctx.Value(userKey).(User); return u }

type Config struct {
	PublicURL, WebDir string
	Secure            bool
}
type App struct {
	Store  *Store
	Config Config
	gate   *limiter
}

func NewApp(s *Store, c Config) *App { return &App{s, c, &limiter{entries: map[string]bucket{}}} }
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := a.Store.DB.PingContext(ctx); err != nil {
			writeJSON(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/login", a.login)
	mux.Handle("GET /api/skill", a.auth(false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="SKILL.md"`)
		fmt.Fprint(w, skills.Sync)
	})))
	mux.Handle("POST /api/password", a.auth(false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Current string `json:"current"`
			Next    string `json:"next"`
		}
		if !decode(w, r, &in) {
			return
		}
		c, _ := r.Cookie("ws_session")
		err := a.Store.ChangePassword(r.Context(), actor(r.Context()), in.Current, in.Next, c.Value)
		respond(w, map[string]bool{"ok": true}, err)
	})))
	mux.Handle("POST /api/logout", a.auth(false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := r.Cookie("ws_session")
		_, err := a.Store.DB.ExecContext(r.Context(), "DELETE FROM credentials WHERE hash=? AND kind='session'", digest(c.Value))
		if err != nil {
			writeError(w, err)
			return
		}
		http.SetCookie(w, a.cookie("", -1))
		writeJSON(w, 200, map[string]bool{"ok": true})
	})))
	mux.Handle("GET /api/me", a.auth(false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, actor(r.Context())) })))
	mux.Handle("GET /api/snapshot", a.auth(false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := a.Store.Snapshot(r.Context(), actor(r.Context()), r.URL.Query().Get("project_id"))
		respond(w, out, err)
	})))
	mux.Handle("PUT /api/items", a.auth(false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in ItemInput
		if !decode(w, r, &in) {
			return
		}
		out, err := a.Store.PutItem(r.Context(), actor(r.Context()), in)
		respond(w, out, err)
	})))
	mux.Handle("GET /api/events", a.auth(false, http.HandlerFunc(a.stream)))
	mux.Handle("GET /api/tokens", a.auth(false, http.HandlerFunc(a.tokens)))
	mux.Handle("POST /api/tokens", a.auth(false, http.HandlerFunc(a.createToken)))
	mux.Handle("DELETE /api/tokens/{id}", a.auth(false, http.HandlerFunc(a.deleteToken)))
	mux.Handle("GET /api/members", a.auth(false, http.HandlerFunc(a.members)))
	mux.Handle("POST /api/members", a.auth(false, http.HandlerFunc(a.createMember)))
	mux.Handle("/mcp", a.auth(true, a.mcpHandler()))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		a.static(w, r)
	})
	return a.security(mux)
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, err error) {
	var e *AppError
	if errors.As(err, &e) {
		writeJSON(w, e.Code, map[string]string{"error": e.Message})
		return
	}
	slog.Error("request failed", "error", err)
	writeJSON(w, 500, map[string]string{"error": "服务暂时不可用，请稍后重试"})
}
func respond(w http.ResponseWriter, out any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		writeError(w, fail(400, "请求 JSON 无效或包含未知字段"))
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		writeError(w, fail(400, "仅允许一个 JSON 对象"))
		return false
	}
	return true
}
func (a *App) cookie(value string, age int) *http.Cookie {
	return &http.Cookie{Name: "ws_session", Value: value, Path: "/", HttpOnly: true, Secure: a.Config.Secure, SameSite: http.SameSiteStrictMode, MaxAge: age}
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Password) > 72 || len(in.Email) > 190 {
		writeError(w, fail(401, "邮箱或密码错误"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	token, err := a.Store.Login(ctx, in.Email, in.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	http.SetCookie(w, a.cookie(token, 86400))
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) auth(mcp bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token, kind string
		if mcp {
			kind = "api"
			v := r.Header.Get("Authorization")
			if strings.HasPrefix(v, "Bearer ") {
				token = strings.TrimPrefix(v, "Bearer ")
			}
		} else {
			kind = "session"
			if c, err := r.Cookie("ws_session"); err == nil {
				token = c.Value
			}
		}
		if token == "" {
			writeError(w, fail(401, "请登录或配置 MCP 访问凭据"))
			return
		}
		u, err := a.Store.Authenticate(r.Context(), token, kind)
		if err != nil {
			writeError(w, err)
			return
		}
		// API credentials never carry administrator acceptance privileges.
		if mcp && u.Role == "admin" {
			u.Role = "member"
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}
func (a *App) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		origin := r.Header.Get("Origin")
		if origin != "" && origin != strings.TrimRight(a.Config.PublicURL, "/") {
			writeError(w, fail(403, "请求来源不被允许"))
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/mcp" {
			next.ServeHTTP(w, r)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			if forwarded := net.ParseIP(r.Header.Get("X-Real-IP")); forwarded != nil {
				host = forwarded.String()
			}
		}
		key := host
		max := 300
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/password" {
			key = "login:" + host
			max = 10
		}
		if !a.gate.Allow(key, max) {
			w.Header().Set("Retry-After", "60")
			writeError(w, fail(429, "请求过于频繁，请稍后重试"))
			return
		}
		if r.URL.Path != "/api/events" {
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			r = r.WithContext(ctx)
		}
		r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
		defer r.Body.Close()
		next.ServeHTTP(w, r)
	})
}
func (a *App) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(a.Config.WebDir, filepath.Clean("/"+r.URL.Path))
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		if filepath.Ext(r.URL.Path) != "" {
			http.NotFound(w, r)
			return
		}
		path = filepath.Join(a.Config.WebDir, "index.html")
	}
	http.ServeFile(w, r, path)
}
func (a *App) stream(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		writeError(w, fail(500, "连接不支持动态更新"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprint(w, ": connected\n\n")
	f.Flush()
	// Bounded per-browser polling is sufficient for the initial small-team deployment.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	ttl := time.NewTimer(30 * time.Minute)
	defer ttl.Stop()
	var last int64 = -1
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ttl.C:
			return
		case <-ticker.C:
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			c, err := r.Cookie("ws_session")
			if err != nil {
				return
			}
			if _, err = a.Store.Authenticate(r.Context(), c.Value, "session"); err != nil {
				return
			}
			var n int64
			if err = a.Store.DB.QueryRowContext(r.Context(), "SELECT revision FROM workspace_revision WHERE id=1").Scan(&n); err != nil {
				return
			}
			if n != last {
				if _, err = fmt.Fprintf(w, "event: change\ndata: %d\n\n", n); err != nil {
					return
				}
				last = n
			} else {
				fmt.Fprint(w, ": heartbeat\n\n")
			}
			f.Flush()
		}
	}
}
func (a *App) tokens(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.DB.QueryContext(r.Context(), "SELECT hash,name,expires_at FROM credentials WHERE user_id=? AND kind='api' ORDER BY created_at DESC", actor(r.Context()).ID)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name string
		var expires time.Time
		if err = rows.Scan(&id, &name, &expires); err != nil {
			writeError(w, err)
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "expires_at": expires})
	}
	if err = rows.Err(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (a *App) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	u := actor(r.Context())
	if u.Role == "viewer" {
		writeError(w, fail(403, "只读成员不能创建开发凭据"))
		return
	}
	token, err := a.Store.Credential(r.Context(), u.ID, "api", in.Name, 90*24*time.Hour)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 201, map[string]string{"token": token, "endpoint": strings.TrimRight(a.Config.PublicURL, "/") + "/mcp"})
}
func (a *App) deleteToken(w http.ResponseWriter, r *http.Request) {
	_, err := a.Store.DB.ExecContext(r.Context(), "DELETE FROM credentials WHERE hash=? AND user_id=? AND kind='api'", r.PathValue("id"), actor(r.Context()).ID)
	respond(w, map[string]bool{"ok": true}, err)
}
func (a *App) members(w http.ResponseWriter, r *http.Request) {
	if actor(r.Context()).Role != "admin" {
		writeError(w, fail(403, "需要管理员权限"))
		return
	}
	rows, err := a.Store.DB.QueryContext(r.Context(), "SELECT id,email,name,role FROM users ORDER BY created_at")
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err = rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			writeError(w, err)
			return
		}
		out = append(out, u)
	}
	if err = rows.Err(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (a *App) createMember(w http.ResponseWriter, r *http.Request) {
	if actor(r.Context()).Role != "admin" {
		writeError(w, fail(403, "需要管理员权限"))
		return
	}
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, err := a.Store.CreateUser(r.Context(), in.Email, in.Name, in.Password, in.Role)
	respond(w, u, err)
}

type bucket struct {
	n     int
	until time.Time
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]bucket
}

func (l *limiter) Allow(key string, max int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.entries) > 1000 {
		for k, v := range l.entries {
			if now.After(v.until) {
				delete(l.entries, k)
			}
		}
		if len(l.entries) > 5000 {
			return false
		}
	}
	b := l.entries[key]
	if now.After(b.until) {
		b = bucket{0, now.Add(time.Minute)}
	}
	b.n++
	l.entries[key] = b
	return b.n <= max
}
func ValidateConfig(c Config) error {
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("PUBLIC_URL must be an HTTP(S) origin")
	}
	if u.Scheme == "https" && !c.Secure {
		return errors.New("HTTPS requires COOKIE_SECURE=true")
	}
	return nil
}
