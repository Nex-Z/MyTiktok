package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct{ DataDir, WebDir, MediaRoot string }
type App struct {
	syncWait     func(context.Context, time.Duration) error
	syncCancel   context.CancelFunc
	syncRunID    int64
	db           *sql.DB
	cfg          Config
	mu           sync.Mutex
	cloud        *P115
	qrs          map[string]*qrSession
	feeds        map[string]*feedSession
	running      map[int64]context.CancelFunc
	wg           sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
	metadataGate chan struct{}
}

func New(cfg Config) (*App, error) {
	db, err := openDB(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	if _, err = db.Exec(syncSchema); err != nil {
		db.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{db: db, cfg: cfg, qrs: map[string]*qrSession{}, feeds: map[string]*feedSession{}, running: map[int64]context.CancelFunc{}, ctx: ctx, cancel: cancel, metadataGate: make(chan struct{}, 1)}
	var cookie string
	_ = db.QueryRow(`SELECT value FROM settings WHERE key='p115_cookie'`).Scan(&cookie)
	if cookie != "" {
		a.cloud = newP115(cookie)
	}
	return a, nil
}
func (a *App) Close() {
	a.cancel()
	a.mu.Lock()
	for _, cancel := range a.running {
		cancel()
	}
	a.mu.Unlock()
	a.wg.Wait()
	a.db.Close()
}
func (a *App) Start() {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-t.C:
				tasks, e := a.syncTasks()
				if e == nil {
					for _, task := range tasks {
						if task.Enabled && time.Now().Unix()-task.LastRun >= int64(task.Interval)*3600 {
							_, _ = a.startSync(task.ID)
						}
					}
				}
				sources, err := a.sources()
				if err == nil {
					for _, s := range sources {
						if s.Interval > 0 && time.Now().Unix()-s.LastScan >= int64(s.Interval)*3600 {
							_, _ = a.startScan(s.ID)
						}
					}
				}
			}
		}
	}()
}
func (a *App) driver(s Source) (Storage, error) {
	if s.Kind == "local" {
		return &Local{Root: s.Root}, nil
	}
	a.mu.Lock()
	p := a.cloud
	a.mu.Unlock()
	if p == nil {
		return nil, errAuth
	}
	return p, nil
}
func (a *App) Handler() http.Handler {
	m := http.NewServeMux()
	a.syncRoutes(m)
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{"status": "ok", "version": "0.1.0"})
	})
	m.HandleFunc("GET /api/sources", func(w http.ResponseWriter, r *http.Request) {
		s, e := a.sources()
		if e != nil {
			fail(w, e)
			return
		}
		jsonOK(w, s)
	})
	m.HandleFunc("POST /api/sources", a.createSource)
	m.HandleFunc("PATCH /api/sources/{id}", a.updateSource)
	m.HandleFunc("DELETE /api/sources/{id}", a.deleteSource)
	m.HandleFunc("GET /api/directories", a.directories)
	m.HandleFunc("POST /api/sources/{id}/scan", func(w http.ResponseWriter, r *http.Request) {
		id, e := a.startScan(pathID(r))
		if e != nil {
			fail(w, e)
			return
		}
		jsonOK(w, map[string]int64{"id": id})
	})
	m.HandleFunc("POST /api/sources/{id}/metadata", a.repairMetadata)
	m.HandleFunc("GET /api/jobs", a.jobs)
	m.HandleFunc("POST /api/jobs/{id}/cancel", a.cancelJob)
	m.HandleFunc("GET /api/feed", a.feed)
	m.HandleFunc("GET /api/library/folders", a.libraryFolders)
	m.HandleFunc("GET /api/videos/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := a.video(pathID(r))
		if e != nil {
			fail(w, e)
			return
		}
		jsonOK(w, v)
	})
	m.HandleFunc("PUT /api/videos/{id}/favorite", a.favorite)
	m.HandleFunc("PUT /api/videos/{id}/like", a.like)
	m.HandleFunc("PUT /api/videos/{id}/history", a.history)
	m.HandleFunc("PUT /api/videos/{id}/hidden", a.hidden)
	m.HandleFunc("GET /api/videos/{id}/play", a.play)
	m.HandleFunc("GET /api/videos/{id}/prepare", a.preparePlayback)
	m.HandleFunc("GET /api/videos/{id}/cover", a.cover)
	m.HandleFunc("POST /api/p115/qr", a.qrStart)
	m.HandleFunc("GET /api/p115/qr/{id}", a.qrPoll)
	m.HandleFunc("PUT /api/p115/cookie", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Cookie string `json:"cookie"`
		}
		if !decode(w, r, &b) {
			return
		}
		if e := a.saveCookie(r.Context(), strings.TrimSpace(b.Cookie)); e != nil {
			fail(w, e)
			return
		}
		jsonOK(w, map[string]bool{"connected": true})
	})
	m.HandleFunc("GET /api/p115/status", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		p := a.cloud
		a.mu.Unlock()
		if p == nil {
			jsonOK(w, map[string]any{"connected": false, "state": "disconnected"})
			return
		}
		e := cloudError(cloudClient(r.Context(), p.cookie).LoginCheck())
		state := "connected"
		if e != nil {
			state = "error"
			if errors.Is(e, errAuth) {
				state = "auth_required"
			}
		}
		jsonOK(w, map[string]any{"connected": e == nil, "state": state})
	})
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, 404, "not_found", "接口不存在") })
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/")
		if key == "" {
			key = "index.html"
		}
		p, e := safePath(a.cfg.WebDir, key)
		if e != nil {
			if filepath.Ext(key) != "" {
				http.NotFound(w, r)
				return
			}
			p = filepath.Join(a.cfg.WebDir, "index.html")
		}
		http.ServeFile(w, r, p)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if origin := r.Header.Get("Origin"); origin != "" {
				u, e := url.Parse(origin)
				if e != nil || !strings.EqualFold(u.Host, r.Host) {
					writeError(w, 403, "cross_origin", "不允许跨站请求")
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeError(w, 403, "cross_origin", "不允许跨站请求")
				return
			}
		}
		m.ServeHTTP(w, r)
	})
}
func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBusy):
		writeError(w, 409, "scan_running", "此媒体源已有扫描任务")
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, os.ErrNotExist):
		writeError(w, 404, "unavailable", "文件或记录不存在")
	case errors.Is(err, errAuth):
		writeError(w, 401, "auth_required", errAuth.Error())
	case errors.Is(err, errRate):
		w.Header().Set("Retry-After", "600")
		writeError(w, 429, "rate_limited", errRate.Error())
	case errors.Is(err, errCloud):
		writeError(w, 502, "upstream_error", errCloud.Error())
	case errors.Is(err, context.Canceled):
		writeError(w, 409, "canceled", "操作已取消")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, 504, "timeout", "操作超时")
	default:
		writeError(w, 500, "internal_error", "操作失败，请检查目录、权限或连接状态")
	}
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		writeError(w, 400, "invalid_request", "请求格式不正确")
		return false
	}
	return true
}
func intParam(s string, fallback int) int {
	v, e := strconv.Atoi(s)
	if e != nil || v < 0 {
		return fallback
	}
	return v
}
func pathID(r *http.Request) int64 { return int64(intParam(r.PathValue("id"), 0)) }
func itoa(i int64) string          { return strconv.FormatInt(i, 10) }
func token() string                { b := make([]byte, 16); _, _ = rand.Read(b); return hex.EncodeToString(b) }
