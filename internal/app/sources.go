package app

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (a *App) createSource(w http.ResponseWriter, r *http.Request) {
	s := Source{Interval: 6}
	if !decode(w, r, &s) {
		return
	}
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" || len(s.Name) > 120 || s.Interval < 0 || s.Interval > 8760 {
		writeError(w, 400, "invalid_source", "请输入名称及 0–8760 小时的扫描间隔")
		return
	}
	switch s.Kind {
	case "local":
		root, e := filepath.Abs(s.Root)
		if e != nil {
			fail(w, e)
			return
		}
		base, e := filepath.Abs(a.cfg.MediaRoot)
		if e != nil {
			fail(w, e)
			return
		}
		rel, e := filepath.Rel(base, root)
		if e != nil {
			writeError(w, 400, "invalid_root", "目录必须位于媒体挂载目录内")
			return
		}
		root, e = safePath(base, rel)
		if e != nil {
			writeError(w, 400, "invalid_root", "目录必须存在且位于媒体挂载目录内")
			return
		}
		info, e := os.Stat(root)
		if e != nil || !info.IsDir() {
			writeError(w, 400, "invalid_root", "请选择文件夹")
			return
		}
		s.Root = root
	case "p115":
		if s.Root == "" {
			s.Root = "0"
		}
		for _, ch := range s.Root {
			if ch < '0' || ch > '9' {
				writeError(w, 400, "invalid_root", "115 目录 ID 必须是数字")
				return
			}
		}
		if _, e := a.driver(s); e != nil {
			fail(w, e)
			return
		}
	default:
		writeError(w, 400, "invalid_source", "不支持的来源类型")
		return
	}
	result, e := a.db.Exec(`INSERT INTO sources(name,kind,root,interval_hours) VALUES(?,?,?,?)`, s.Name, s.Kind, s.Root, s.Interval)
	if e != nil {
		writeError(w, 409, "source_exists", "目录已添加或配置无效")
		return
	}
	s.ID, _ = result.LastInsertId()
	_, _ = a.startScan(s.ID)
	jsonOK(w, s)
}
func (a *App) updateSource(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name     string `json:"name"`
		Interval int    `json:"intervalHours"`
	}
	if !decode(w, r, &b) {
		return
	}
	if strings.TrimSpace(b.Name) == "" || b.Interval < 0 || b.Interval > 8760 {
		writeError(w, 400, "invalid_source", "名称或扫描间隔无效")
		return
	}
	result, e := a.db.Exec(`UPDATE sources SET name=?,interval_hours=? WHERE id=?`, b.Name, b.Interval, pathID(r))
	if e != nil {
		fail(w, e)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, 404, "not_found", "媒体源不存在")
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
func (a *App) deleteSource(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	a.mu.Lock()
	_, busy := a.running[id]
	if busy || a.syncCancel != nil {
		a.mu.Unlock()
		writeError(w, 409, "scan_running", "请先取消正在运行的扫描或同步，并等待任务结束")
		return
	}
	_, e := a.db.Exec(`DELETE FROM sources WHERE id=?`, id)
	a.mu.Unlock()
	if e != nil {
		fail(w, e)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
func (a *App) directories(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	key := r.URL.Query().Get("key")
	var d Storage
	var e error
	if kind == "local" {
		d = &Local{Root: a.cfg.MediaRoot}
	} else if kind == "p115" {
		d, e = a.driver(Source{Kind: "p115"})
		if key == "" {
			key = "0"
		}
	} else {
		writeError(w, 400, "invalid_source", "来源类型无效")
		return
	}
	if e != nil {
		fail(w, e)
		return
	}
	page, e := d.List(r.Context(), key, r.URL.Query().Get("cursor"))
	if e != nil {
		fail(w, e)
		return
	}
	out := []Entry{}
	for _, v := range page.Entries {
		if v.Dir {
			out = append(out, v)
		}
	}
	page.Entries = out
	root := key
	if kind == "local" {
		root, e = safePath(a.cfg.MediaRoot, key)
		if e != nil {
			fail(w, e)
			return
		}
	}
	jsonOK(w, map[string]any{"entries": page.Entries, "next": page.Next, "root": root})
}
func (a *App) jobs(w http.ResponseWriter, r *http.Request) {
	rows, e := a.db.Query(`SELECT id,source_id,status,scanned,errors,message,started_at,finished_at FROM jobs ORDER BY id DESC LIMIT 100`)
	if e != nil {
		fail(w, e)
		return
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		var j Job
		if e = rows.Scan(&j.ID, &j.SourceID, &j.Status, &j.Scanned, &j.Errors, &j.Message, &j.StartedAt, &j.FinishedAt); e != nil {
			fail(w, e)
			return
		}
		out = append(out, j)
	}
	jsonOK(w, out)
}
func (a *App) cancelJob(w http.ResponseWriter, r *http.Request) {
	var sourceID int64
	var status string
	e := a.db.QueryRow(`SELECT source_id,status FROM jobs WHERE id=?`, pathID(r)).Scan(&sourceID, &status)
	if e != nil {
		fail(w, e)
		return
	}
	a.mu.Lock()
	if status == "running" || status == "cooling" {
		if c := a.running[sourceID]; c != nil {
			c()
		}
	}
	a.mu.Unlock()
	jsonOK(w, map[string]bool{"ok": true})
}

var errBusy = errors.New("scan already running")
