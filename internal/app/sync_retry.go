package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

func (a *App) syncPause(ctx context.Context, run int64, delay time.Duration, message string) error {
	if _, e := a.db.Exec(`UPDATE sync_runs SET status='cooling',message=? WHERE id=?`, message, run); e != nil {
		return e
	}
	wait := a.syncWait
	if wait == nil {
		wait = sleep
	}
	if e := wait(ctx, delay); e != nil {
		return e
	}
	_, e := a.db.Exec(`UPDATE sync_runs SET status='running',message='' WHERE id=?`, run)
	return e
}
func (a *App) syncBackoff(ctx context.Context, run int64, attempt int, cause error) error {
	if errors.Is(cause, errAuth) || ctx.Err() != nil {
		return cause
	}
	delay := time.Duration(attempt) * 30 * time.Second
	if errors.Is(cause, errRate) {
		delay = 10 * time.Minute
	}
	return a.syncPause(ctx, run, delay, fmt.Sprintf("等待 %d 秒后第 %d 次尝试；已完成记录保留", int(delay.Seconds()), attempt+1))
}
func (a *App) syncList(ctx context.Context, p syncCloud, parent, cursor string, run int64) (Page, error) {
	var page Page
	var e error
	for attempt := 1; attempt <= 3; attempt++ {
		page, e = p.List(ctx, parent, cursor)
		if e == nil || errors.Is(e, errAuth) || ctx.Err() != nil {
			return page, e
		}
		if attempt < 3 {
			if e = a.syncBackoff(ctx, run, attempt, e); e != nil {
				return Page{}, e
			}
		}
	}
	return page, e
}
func (a *App) syncFind(ctx context.Context, p syncCloud, parent, name string, run int64) (Entry, bool, error) {
	cursor := ""
	for {
		page, e := a.syncList(ctx, p, parent, cursor, run)
		if e != nil {
			return Entry{}, false, e
		}
		for _, v := range page.Entries {
			if v.Name == name {
				return v, true, nil
			}
		}
		if page.Next == "" {
			return Entry{}, false, nil
		}
		cursor = page.Next
	}
}
func (a *App) syncMkdir(ctx context.Context, p syncCloud, parent, name string, run int64) (string, error) {
	var cause error
	for attempt := 1; attempt <= 3; attempt++ {
		// Reconcile even a failed create: the server may have created the directory.
		v, exists, e := a.syncFind(ctx, p, parent, name, run)
		if e != nil {
			return "", e
		}
		if exists {
			if !v.Dir {
				return "", errCloud
			}
			return v.Key, nil
		}
		id, e := p.Mkdir(ctx, parent, name)
		if e == nil {
			return id, nil
		}
		cause = e
		if errors.Is(e, errAuth) || ctx.Err() != nil {
			return "", e
		}
		if attempt < 3 {
			if e = a.syncBackoff(ctx, run, attempt, e); e != nil {
				return "", e
			}
		}
	}
	return "", cause
}

// Do not blindly repeat a write whose response was lost. Read the destination first.
func (a *App) syncUpload(ctx context.Context, p syncCloud, parent string, entry Entry, f *os.File, run int64) (string, string, error) {
	var cause error
	for attempt := 1; attempt <= 3; attempt++ {
		if e := ctx.Err(); e != nil {
			return "unconfirmed", "已取消，重新执行时先核对云端", e
		}
		if attempt > 1 {
			v, exists, e := a.syncFind(ctx, p, parent, entry.Name, run)
			if e != nil {
				return "unconfirmed", "无法核对云端结果，已停止重复上传", e
			}
			if exists {
				if !v.Dir && v.Size == entry.Size {
					return "uploaded", "响应异常后已核对云端同名文件及大小", nil
				}
				return "skipped", "云端已有同名项，按规则保留；大小或类型不同", nil
			}
		}
		if _, e := f.Seek(0, 0); e != nil {
			return "failed", "无法读取本地文件", e
		}
		uploadCtx, cancel := context.WithTimeout(ctx, 6*time.Hour)
		e := p.Upload(uploadCtx, parent, entry.Name, f)
		cancel()
		if e == nil {
			return "uploaded", "上传成功", nil
		}
		cause = e
		if errors.Is(e, errAuth) || ctx.Err() != nil {
			return "unconfirmed", "上传结果待核对，重新连接或继续任务后核对云端", e
		}
		if attempt < 3 {
			if e = a.syncBackoff(ctx, run, attempt, e); e != nil {
				return "unconfirmed", "上传结果待核对", e
			}
		}
	}
	// Also reconcile the last attempt instead of falsely reporting a lost response as failure.
	v, exists, e := a.syncFind(ctx, p, parent, entry.Name, run)
	if e != nil {
		return "unconfirmed", "三次尝试后仍无法核对云端，请重新核对并继续", e
	}
	if exists {
		if !v.Dir && v.Size == entry.Size {
			return "uploaded", "已核对云端同名文件及大小", nil
		}
		return "skipped", "云端已有同名项，按规则保留；大小或类型不同", nil
	}
	return "failed", "三次尝试后云端仍未发现文件，可重新核对并继续", cause
}
