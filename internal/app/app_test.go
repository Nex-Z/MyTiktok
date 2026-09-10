package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) *App {
	t.Helper()
	a, e := New(Config{DataDir: t.TempDir(), MediaRoot: t.TempDir(), WebDir: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(a.Close)
	return a
}
func request(t *testing.T, a *App, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func mustExec(t *testing.T, a *App, q string, args ...any) {
	t.Helper()
	if _, e := a.db.Exec(q, args...); e != nil {
		t.Fatal(e)
	}
}
func seedSource(t *testing.T, a *App) Source {
	t.Helper()
	mustExec(t, a, `INSERT INTO sources(id,name,kind,root,interval_hours) VALUES(1,'Local','local',?,0)`, a.cfg.MediaRoot)
	s, e := a.source(1)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestLocalScanPersistenceAndPartialFailure(t *testing.T) {
	a := testApp(t)
	s := seedSource(t, a)
	file := filepath.Join(s.Root, "clip.mp4")
	if e := os.WriteFile(file, []byte("invalid video is still indexed"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := a.scan(context.Background(), s, 1); e != nil {
		t.Fatal(e)
	}
	mustExec(t, a, `UPDATE videos SET hidden=1`)
	mustExec(t, a, `INSERT INTO favorites VALUES(1,1)`)
	mustExec(t, a, `INSERT INTO history VALUES(1,3.5,1)`)
	if e := a.scan(context.Background(), s, 2); e != nil {
		t.Fatal(e)
	}
	v, e := a.video(1)
	if e != nil || !v.Hidden || !v.Favorite || v.Position != 3.5 {
		t.Fatalf("state lost: %+v %v", v, e)
	}
	var n int
	a.db.QueryRow(`SELECT count(*) FROM videos`).Scan(&n)
	if n != 1 {
		t.Fatal("duplicate", n)
	}
	// Root disappearance fails a scan without marking its indexed contents missing.
	s.Root = filepath.Join(s.Root, "missing")
	if e := a.scan(context.Background(), s, 3); e == nil {
		t.Fatal("expected missing root error")
	}
	v, _ = a.video(1)
	if !v.Available {
		t.Fatal("failed scan removed media")
	}
	s.Root = a.cfg.MediaRoot
	if e := os.Remove(file); e != nil {
		t.Fatal(e)
	}
	if e := a.scan(context.Background(), s, 4); e != nil {
		t.Fatal(e)
	}
	v, _ = a.video(1)
	if v.Available {
		t.Fatal("successful scan did not mark missing")
	}
}
func TestFeedTenThousandSnapshot(t *testing.T) {
	a := testApp(t)
	seedSource(t, a)
	tx, e := a.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	stmt, e := tx.Prepare(`INSERT INTO videos(id,source_id,file_key,title,size,modified,indexed_at) VALUES(?,1,?,'video',1,1,1)`)
	if e != nil {
		t.Fatal(e)
	}
	for i := 1; i <= 10000; i++ {
		if _, e = stmt.Exec(i, fmt.Sprint(i)); e != nil {
			t.Fatal(e)
		}
	}
	stmt.Close()
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	cursor := ""
	seen := map[int64]bool{}
	pages := 0
	for {
		path := "/api/feed?limit=50"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		w := request(t, a, "GET", path, nil)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var f struct {
			Items []Video `json:"items"`
			Next  string  `json:"next"`
			Total int     `json:"total"`
		}
		if e = json.Unmarshal(w.Body.Bytes(), &f); e != nil {
			t.Fatal(e)
		}
		if len(f.Items) > 50 {
			t.Fatal("unbounded page")
		}
		for _, v := range f.Items {
			if seen[v.ID] {
				t.Fatal("duplicate", v.ID)
			}
			seen[v.ID] = true
		}
		pages++
		if pages == 1 {
			mustExec(t, a, `INSERT INTO videos(id,source_id,file_key,title,size,modified,indexed_at) VALUES(10001,1,'new','new',1,1,2)`)
		}
		cursor = f.Next
		if cursor == "" {
			break
		}
	}
	if len(seen) != 10000 || seen[10001] {
		t.Fatal("snapshot changed", len(seen))
	}
	t.Logf("10000 videos, %d pages: %s", pages, time.Since(start))
}
func TestLocalRangeAndConfinement(t *testing.T) {
	a := testApp(t)
	s := seedSource(t, a)
	os.WriteFile(filepath.Join(s.Root, "clip.mp4"), []byte("0123456789"), 0600)
	mustExec(t, a, `INSERT INTO videos(source_id,file_key,title,size,modified,indexed_at) VALUES(1,'clip.mp4','clip',10,1,1)`)
	for _, tc := range []struct {
		rangeHeader string
		status      int
		body        string
	}{{"bytes=2-5", 206, "2345"}, {"bytes=-3", 206, "789"}, {"bytes=99-", 416, ""}} {
		r := httptest.NewRequest("GET", "/api/videos/1/play", nil)
		r.Header.Set("Range", tc.rangeHeader)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
		if tc.body != "" && w.Body.String() != tc.body {
			t.Fatal(w.Body.String())
		}
	}
	if _, e := safePath(s.Root, "../library.db"); e == nil {
		t.Fatal("traversal allowed")
	}
	mustExec(t, a, `UPDATE videos SET file_key='../secret' WHERE id=1`)
	if w := request(t, a, "GET", "/api/videos/1/play", nil); w.Code == 200 {
		t.Fatal("escaped root")
	}
	w := request(t, a, "POST", "/api/sources", map[string]any{"name": "escape", "kind": "local", "root": filepath.Dir(s.Root), "intervalHours": 0})
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestProxyRangeHeaders(t *testing.T) {
	if mediaHTTP() != mediaHTTP() || mediaTransport.DisableKeepAlives {
		t.Fatal("media client does not reuse connections")
	}
	for _, status := range []int{200, 206, 416} {
		w := httptest.NewRecorder()
		resp := &http.Response{StatusCode: status, Header: http.Header{"Content-Range": {"bytes 2-4/10"}, "Accept-Ranges": {"bytes"}, "Set-Cookie": {"secret"}}, Body: io.NopCloser(strings.NewReader("234"))}
		copyStream(w, httptest.NewRequest("GET", "/", nil), resp)
		if w.Code != status || w.Header().Get("Content-Range") == "" || w.Header().Get("Set-Cookie") != "" {
			t.Fatal("bad proxy response")
		}
	}
}

func TestPrepareLocalPlaybackDoesNotReadMedia(t *testing.T) {
	a := testApp(t)
	seedSource(t, a)
	mustExec(t, a, `INSERT INTO videos(source_id,file_key,title,size,modified,indexed_at) VALUES(1,'missing.mp4','missing',1,1,1)`)
	w := request(t, a, "GET", "/api/videos/1/prepare", nil)
	if w.Code != http.StatusNoContent {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestMutationAndRestart(t *testing.T) {
	a := testApp(t)
	seedSource(t, a)
	mustExec(t, a, `INSERT INTO videos(id,source_id,file_key,title,size,modified,duration,indexed_at,seen_job) VALUES(1,1,'one','one',1,1,100,1,1)`)
	for _, path := range []string{"favorite", "hidden"} {
		w := request(t, a, "PUT", "/api/videos/1/"+path, map[string]bool{"value": true})
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	w := request(t, a, "PUT", "/api/videos/1/history", map[string]float64{"position": 200})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	v, _ := a.video(1)
	if v.Position != 100 {
		t.Fatal("progress not clamped")
	}
	mustExec(t, a, `INSERT INTO jobs(source_id,status,started_at) VALUES(1,'running',1)`)
	// A second connection simulates startup recovery, without destroying the test app.
	db, e := openDB(a.cfg.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var status string
	db.QueryRow(`SELECT status FROM jobs`).Scan(&status)
	if status != "interrupted" {
		t.Fatal(status)
	}
	v, _ = a.video(1)
	if !v.Favorite || !v.Hidden || v.Position != 100 {
		t.Fatal("persistence failed")
	}
	w = request(t, a, "DELETE", "/api/sources/1", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var n int
	a.db.QueryRow(`SELECT count(*) FROM history`).Scan(&n)
	if n != 0 {
		t.Fatal("cascade failed")
	}
}
func TestCancelDoesNotPrune(t *testing.T) {
	a := testApp(t)
	s := seedSource(t, a)
	mustExec(t, a, `INSERT INTO videos(source_id,file_key,title,size,modified,indexed_at) VALUES(1,'old','old',1,1,1)`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := a.scan(ctx, s, 1); e != context.Canceled {
		t.Fatal(e)
	}
	v, _ := a.video(1)
	if !v.Available {
		t.Fatal("canceled scan pruned")
	}
}
func TestOriginProtection(t *testing.T) {
	a := testApp(t)
	r := httptest.NewRequest("POST", "/api/sources", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://evil.invalid")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
