package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const syncSchema = `
CREATE TABLE IF NOT EXISTS sync_tasks(id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,target TEXT NOT NULL,interval_hours INTEGER NOT NULL,enabled INTEGER NOT NULL,last_run INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS sync_runs(id INTEGER PRIMARY KEY AUTOINCREMENT,task_id INTEGER NOT NULL REFERENCES sync_tasks(id) ON DELETE CASCADE,status TEXT NOT NULL,uploaded INTEGER NOT NULL DEFAULT 0,skipped INTEGER NOT NULL DEFAULT 0,failed INTEGER NOT NULL DEFAULT 0,current_file TEXT NOT NULL DEFAULT '',message TEXT NOT NULL DEFAULT '',started_at INTEGER NOT NULL,finished_at INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS sync_records(id INTEGER PRIMARY KEY AUTOINCREMENT,run_id INTEGER NOT NULL REFERENCES sync_runs(id) ON DELETE CASCADE,path TEXT NOT NULL,status TEXT NOT NULL,message TEXT NOT NULL,size INTEGER NOT NULL,created_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS sync_records_run ON sync_records(run_id,id);
UPDATE sync_runs SET failed=failed+(SELECT count(*) FROM sync_records WHERE run_id=sync_runs.id AND status='uploading');
UPDATE sync_records SET status='unconfirmed',message='服务重启时上传尚未确认；请重新核对并继续' WHERE status='uploading';
UPDATE sync_runs SET status='interrupted',message='服务重启；已上传的文件保留，下次按同名规则跳过',finished_at=unixepoch() WHERE status IN ('running','cooling','queued');
`

type SyncTask struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	SourceID int64  `json:"sourceId"`
	Target   string `json:"target"`
	Interval int    `json:"intervalHours"`
	Enabled  bool   `json:"enabled"`
	LastRun  int64  `json:"lastRun"`
}
type SyncRun struct {
	ID       int64  `json:"id"`
	TaskID   int64  `json:"taskId"`
	Status   string `json:"status"`
	Uploaded int    `json:"uploaded"`
	Skipped  int    `json:"skipped"`
	Failed   int    `json:"failed"`
	Current  string `json:"currentFile"`
	Message  string `json:"message"`
	Started  int64  `json:"startedAt"`
	Finished int64  `json:"finishedAt"`
}
type syncCloud interface {
	List(context.Context, string, string) (Page, error)
	Mkdir(context.Context, string, string) (string, error)
	Upload(context.Context, string, string, *os.File) error
}

func (a *App) syncTasks() ([]SyncTask, error) {
	rows, e := a.db.Query(`SELECT id,name,source_id,target,interval_hours,enabled,last_run FROM sync_tasks ORDER BY id DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SyncTask{}
	for rows.Next() {
		var t SyncTask
		if e = rows.Scan(&t.ID, &t.Name, &t.SourceID, &t.Target, &t.Interval, &t.Enabled, &t.LastRun); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (a *App) syncRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/sync/tasks", func(w http.ResponseWriter, r *http.Request) {
		v, e := a.syncTasks()
		if e != nil {
			fail(w, e)
			return
		}
		jsonOK(w, v)
	})
	m.HandleFunc("POST /api/sync/tasks", func(w http.ResponseWriter, r *http.Request) {
		var t SyncTask
		if !decode(w, r, &t) {
			return
		}
		s, e := a.source(t.SourceID)
		if e != nil || s.Kind != "local" || strings.TrimSpace(t.Name) == "" || len(t.Name) > 200 || t.Interval < 1 || t.Interval > 8760 || t.Target == "" || strings.Trim(t.Target, "0123456789") != "" {
			writeError(w, 400, "invalid_task", "请选择本地媒体源、115 目录和 1–8760 小时的周期")
			return
		}
		res, e := a.db.Exec(`INSERT INTO sync_tasks(name,source_id,target,interval_hours,enabled,last_run) VALUES(?,?,?,?,?,unixepoch())`, t.Name, t.SourceID, t.Target, t.Interval, t.Enabled)
		if e != nil {
			fail(w, e)
			return
		}
		id, _ := res.LastInsertId()
		jsonOK(w, map[string]int64{"id": id})
	})
	m.HandleFunc("PATCH /api/sync/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Enabled  bool `json:"enabled"`
			Interval int  `json:"intervalHours"`
		}
		if !decode(w, r, &b) {
			return
		}
		if b.Interval < 1 || b.Interval > 8760 {
			writeError(w, 400, "invalid_interval", "扫描周期无效")
			return
		}
		_, e := a.db.Exec(`UPDATE sync_tasks SET enabled=?,interval_hours=? WHERE id=?`, b.Enabled, b.Interval, pathID(r))
		if e != nil {
			fail(w, e)
			return
		}
		jsonOK(w, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/sync/tasks/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		id, e := a.startSync(pathID(r))
		if errors.Is(e, errBusy) {
			writeError(w, 409, "sync_running", "已有同步正在执行，请等待完成或取消")
			return
		}
		if e != nil {
			fail(w, e)
			return
		}
		jsonOK(w, map[string]int64{"id": id})
	})
	m.HandleFunc("POST /api/sync/runs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		if a.syncRunID == pathID(r) && a.syncCancel != nil {
			a.syncCancel()
		}
		a.mu.Unlock()
		jsonOK(w, map[string]bool{"ok": true})
	})
	m.HandleFunc("GET /api/sync/runs", a.syncRuns)
	m.HandleFunc("GET /api/sync/runs/{id}/records", a.syncRecords)
}
func (a *App) syncRuns(w http.ResponseWriter, r *http.Request) {
	before := intParam(r.URL.Query().Get("before"), 0)
	rows, e := a.db.Query(`SELECT id,task_id,status,uploaded,skipped,failed,current_file,message,started_at,finished_at FROM sync_runs WHERE (?=0 OR id<?) ORDER BY id DESC LIMIT 50`, before, before)
	if e != nil {
		fail(w, e)
		return
	}
	defer rows.Close()
	out := []SyncRun{}
	for rows.Next() {
		var v SyncRun
		if e = rows.Scan(&v.ID, &v.TaskID, &v.Status, &v.Uploaded, &v.Skipped, &v.Failed, &v.Current, &v.Message, &v.Started, &v.Finished); e != nil {
			fail(w, e)
			return
		}
		out = append(out, v)
	}
	jsonOK(w, out)
}
func (a *App) syncRecords(w http.ResponseWriter, r *http.Request) {
	rows, e := a.db.Query(`SELECT id,path,status,message,size,created_at FROM sync_records WHERE run_id=? AND id>? ORDER BY id LIMIT 100`, pathID(r), intParam(r.URL.Query().Get("after"), 0))
	if e != nil {
		fail(w, e)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, size, at int64
		var path, status, msg string
		if e = rows.Scan(&id, &path, &status, &msg, &size, &at); e != nil {
			fail(w, e)
			return
		}
		out = append(out, map[string]any{"id": id, "path": path, "status": status, "message": msg, "size": size, "createdAt": at})
	}
	jsonOK(w, out)
}
func (a *App) startSync(id int64) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.syncCancel != nil {
		return 0, errBusy
	}
	if a.ctx.Err() != nil {
		return 0, a.ctx.Err()
	}
	if a.cloud == nil {
		return 0, errAuth
	}
	var t SyncTask
	e := a.db.QueryRow(`SELECT id,name,source_id,target,interval_hours,enabled,last_run FROM sync_tasks WHERE id=?`, id).Scan(&t.ID, &t.Name, &t.SourceID, &t.Target, &t.Interval, &t.Enabled, &t.LastRun)
	if e != nil {
		return 0, e
	}
	s, e := a.source(t.SourceID)
	if e != nil {
		return 0, e
	}
	if s.Kind != "local" {
		return 0, sql.ErrNoRows
	}
	rel, e := filepath.Rel(a.cfg.MediaRoot, s.Root)
	if e != nil {
		return 0, e
	}
	if _, e = safePath(a.cfg.MediaRoot, rel); e != nil {
		return 0, e
	}
	res, e := a.db.Exec(`INSERT INTO sync_runs(task_id,status,started_at) VALUES(?,'running',unixepoch())`, id)
	if e != nil {
		return 0, e
	}
	run, _ := res.LastInsertId()
	_, _ = a.db.Exec(`UPDATE sync_tasks SET last_run=unixepoch() WHERE id=?`, id)
	ctx, cancel := context.WithCancel(a.ctx)
	a.syncCancel = cancel
	a.syncRunID = run
	p := a.cloud
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer cancel()
		e := a.copyTree(ctx, s.Root, t.Target, run, p)
		status, msg := "completed", "同步完成"
		var failures int
		_ = a.db.QueryRow(`SELECT failed FROM sync_runs WHERE id=?`, run).Scan(&failures)
		if failures > 0 {
			status, msg = "partial", "部分文件失败，可重新执行"
		}
		if e != nil {
			status, msg = "failed", "同步已暂停：重试未成功，请查看明细后重新核对并继续"
			if errors.Is(e, errAuth) {
				msg = errAuth.Error()
			}
			if errors.Is(e, context.Canceled) {
				status, msg = "canceled", "已取消；已上传文件保留"
			}
		}
		_, _ = a.db.Exec(`UPDATE sync_runs SET status=?,message=?,current_file='',finished_at=unixepoch() WHERE id=?`, status, msg, run)
		a.mu.Lock()
		a.syncCancel = nil
		a.syncRunID = 0
		a.mu.Unlock()
	}()
	return run, nil
}
func (a *App) syncRecord(run int64, path, status, msg string, size int64) error {
	tx, e := a.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var pending int64
	e = tx.QueryRow(`SELECT id FROM sync_records WHERE run_id=? AND path=? AND status='uploading' ORDER BY id DESC LIMIT 1`, run, path).Scan(&pending)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if pending != 0 && status != "uploading" {
		_, e = tx.Exec(`UPDATE sync_records SET status=?,message=?,size=?,created_at=unixepoch() WHERE id=?`, status, msg, size, pending)
	} else {
		_, e = tx.Exec(`INSERT INTO sync_records(run_id,path,status,message,size,created_at) VALUES(?,?,?,?,?,unixepoch())`, run, path, status, msg, size)
	}
	if e != nil {
		return e
	}
	if status == "uploading" {
		return tx.Commit()
	}
	col := "failed"
	if status == "uploaded" {
		col = "uploaded"
	}
	if status == "skipped" {
		col = "skipped"
	}
	_, e = tx.Exec(`UPDATE sync_runs SET `+col+`=`+col+`+1 WHERE id=?`, run)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (a *App) copyTree(ctx context.Context, root, target string, run int64, p syncCloud) error {
	// Always list every cloud page before any upload into that directory.
	type dir struct{ local, cloud string }
	queue := []dir{{".", target}}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		names := map[string]Entry{}
		cursor := ""
		for {
			page, e := a.syncList(ctx, p, d.cloud, cursor, run)
			if e != nil {
				return e
			}
			for _, v := range page.Entries {
				names[v.Name] = v
			}
			if page.Next == "" {
				break
			}
			cursor = page.Next
		}
		local := &Local{Root: root}
		cursor = ""
		for {
			page, e := local.List(ctx, d.local, cursor)
			if e != nil {
				return e
			}
			for _, entry := range page.Entries {
				if e = ctx.Err(); e != nil {
					return e
				}
				_, _ = a.db.Exec(`UPDATE sync_runs SET current_file=? WHERE id=?`, entry.Key, run)
				existing, exists := names[entry.Name]
				if entry.Dir {
					if exists && !existing.Dir {
						if e = a.syncRecord(run, entry.Key, "failed", "云端同名项是文件，无法同步子目录", 0); e != nil {
							return e
						}
						continue
					}
					cid := existing.Key
					if !exists {
						cid, e = a.syncMkdir(ctx, p, d.cloud, entry.Name, run)
						if e != nil {
							if re := a.syncRecord(run, entry.Key, "failed", "目录创建未确认；重新执行时核对", 0); re != nil {
								return re
							}
							return e
						}
						names[entry.Name] = Entry{Key: cid, Name: entry.Name, Dir: true}
					}
					queue = append(queue, dir{entry.Key, cid})
					continue
				}
				if exists {
					if e = a.syncRecord(run, entry.Key, "skipped", "目标目录已存在同名项", entry.Size); e != nil {
						return e
					}
					continue
				}
				path, e := safePath(root, entry.Key)
				if e != nil {
					return e
				}
				stat, statErr := os.Stat(path)
				if statErr != nil || !stat.Mode().IsRegular() {
					if e = a.syncRecord(run, entry.Key, "failed", "只支持可读取的普通文件", entry.Size); e != nil {
						return e
					}
					continue
				}
				f, e := os.Open(path)
				if e != nil {
					if e = a.syncRecord(run, entry.Key, "failed", "无法读取本地文件", entry.Size); e != nil {
						return e
					}
					continue
				}
				info, e := f.Stat()
				if e != nil || !info.Mode().IsRegular() {
					f.Close()
					return errors.New("not regular file")
				}
				if e = a.syncRecord(run, entry.Key, "uploading", "排队上传，结果尚未确认", entry.Size); e != nil {
					f.Close()
					return e
				}
				if e = a.syncPause(ctx, run, 3*time.Second, "文件间隔 3 秒，串行上传"); e != nil {
					f.Close()
					if re := a.syncRecord(run, entry.Key, "unconfirmed", "已取消，可重新核对并继续", entry.Size); re != nil {
						return re
					}
					return e
				}
				status, message, uploadErr := a.syncUpload(ctx, p, d.cloud, entry, f, run)
				f.Close()
				if e = a.syncRecord(run, entry.Key, status, message, entry.Size); e != nil {
					return e
				}
				if uploadErr != nil {
					// Stop after exhausting a file's attempts rather than hammering the next 499.
					return uploadErr
				}
				names[entry.Name] = entry

			}
			if page.Next == "" {
				break
			}
			cursor = page.Next
		}
	}
	return nil
}
