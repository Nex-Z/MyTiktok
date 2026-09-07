package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

func (a *App) repairMetadata(w http.ResponseWriter, r *http.Request) {
	id, e := a.startMetadataRepair(pathID(r))
	if e != nil {
		fail(w, e)
		return
	}
	jsonOK(w, map[string]int64{"id": id})
}
func (a *App) startMetadataRepair(sourceID int64) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.running[sourceID]; ok {
		return 0, errBusy
	}
	if e := a.ctx.Err(); e != nil {
		return 0, e
	}
	s, e := a.source(sourceID)
	if e != nil {
		return 0, e
	}
	d, e := func() (Storage, error) {
		if s.Kind == "local" {
			return &Local{Root: s.Root}, nil
		}
		if a.cloud == nil {
			return nil, errAuth
		}
		return a.cloud, nil
	}()
	if e != nil {
		return 0, e
	}
	res, e := a.db.Exec(`INSERT INTO jobs(source_id,status,started_at,message) VALUES(?,'running',unixepoch(),'正在补全元数据')`, sourceID)
	if e != nil {
		return 0, e
	}
	job, _ := res.LastInsertId()
	ctx, cancel := context.WithCancel(a.ctx)
	a.running[sourceID] = cancel
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer cancel()
		defer func() { a.mu.Lock(); delete(a.running, sourceID); a.mu.Unlock() }()
		count, failed := 0, 0
		err := func() error {
			rows, e := a.db.Query(`SELECT id,file_key,size FROM videos v WHERE source_id=? AND available=1 AND ((`+mediaTypeSQL()+`!='image' AND duration<=0) OR (`+mediaTypeSQL()+`!='audio' AND (width<=0 OR height<=0 OR cover=''))) ORDER BY id DESC`, sourceID)
			if e != nil {
				return e
			}
			items := []metadataItem{}
			for rows.Next() {
				var item metadataItem
				if e = rows.Scan(&item.id, &item.e.Key, &item.e.Size); e != nil {
					rows.Close()
					return e
				}
				items = append(items, item)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			for _, item := range items {
				if e = ctx.Err(); e != nil {
					return e
				}
				_, _ = a.db.Exec(`UPDATE jobs SET message=? WHERE id=?`, fmt.Sprintf("补全元数据 %d/%d", count+1, len(items)), job)
				e = a.metadata(ctx, d, s, item)
				if ctx.Err() != nil {
					return ctx.Err()
				}
				count++
				if e != nil {
					failed++
				}
				_, _ = a.db.Exec(`UPDATE jobs SET scanned=?,errors=? WHERE id=?`, count, failed, job)
				if errors.Is(e, errAuth) || errors.Is(e, errRate) {
					return e
				}
				if s.Kind == "p115" {
					if e = sleep(ctx, 2*time.Second); e != nil {
						return e
					}
				}
			}
			return nil
		}()
		state, msg := "completed", fmt.Sprintf("元数据补全完成：处理 %d，未完整补全 %d", count, failed)
		if err != nil {
			state, msg = "failed", "元数据补全中断，已成功结果保留"
			if errors.Is(err, context.Canceled) {
				state, msg = "canceled", "元数据补全已取消"
			}
			if errors.Is(err, errAuth) || errors.Is(err, errRate) {
				msg = err.Error()
			}
		}
		_, _ = a.db.Exec(`UPDATE jobs SET status=?,message=?,finished_at=unixepoch() WHERE id=?`, state, msg, job)
	}()
	return job, nil
}
