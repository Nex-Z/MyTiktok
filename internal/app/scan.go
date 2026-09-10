package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"time"
)

func (a *App) startScan(sourceID int64) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.running[sourceID]; ok {
		return 0, errBusy
	}
	if a.ctx.Err() != nil {
		return 0, context.Canceled
	}
	s, e := a.source(sourceID)
	if e != nil {
		return 0, e
	}
	result, e := a.db.Exec(`INSERT INTO jobs(source_id,status,started_at) VALUES(?,'running',unixepoch())`, s.ID)
	if e != nil {
		return 0, e
	}
	id, _ := result.LastInsertId()
	ctx, cancel := context.WithCancel(a.ctx)
	a.running[sourceID] = cancel
	_, _ = a.db.Exec(`UPDATE sources SET last_scan=unixepoch() WHERE id=?`, s.ID)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer cancel()
		defer func() { a.mu.Lock(); delete(a.running, sourceID); a.mu.Unlock() }()
		e := a.scan(ctx, s, id)
		status, message := "completed", "扫描完成"
		if e != nil {
			status = "failed"
			message = "扫描失败，已保留原有索引，请检查目录或连接"
			if errors.Is(e, context.Canceled) {
				status = "canceled"
				message = "扫描已取消"
			} else if errors.Is(e, errAuth) {
				message = errAuth.Error()
			} else if errors.Is(e, errRate) {
				message = errRate.Error()
			}
		}
		_, _ = a.db.Exec(`UPDATE jobs SET status=?,message=?,finished_at=unixepoch() WHERE id=?`, status, message, id)
	}()
	return id, nil
}

type metadataItem struct {
	id int64
	e  Entry
}

func (a *App) scan(ctx context.Context, s Source, job int64) error {
	d, err := a.driver(s)
	if err != nil {
		return err
	}
	root := s.Root
	if s.Kind == "local" {
		root = "."
	}
	type directory struct{ key, folder string }
	queue := []directory{{root, ""}}
	visited := map[string]bool{}
	count := 0
	for len(queue) > 0 {
		current := queue[0]
		key := current.key
		queue = queue[1:]
		if visited[key] {
			continue
		}
		visited[key] = true
		cursor := ""
		for {
			if err = ctx.Err(); err != nil {
				return err
			}
			page, e := d.List(ctx, key, cursor)
			if errors.Is(e, errRate) {
				_, _ = a.db.Exec(`UPDATE jobs SET status='cooling',message='115 请求受限，10 分钟后重试' WHERE id=?`, job)
				if e = sleep(ctx, 10*time.Minute); e != nil {
					return e
				}
				_, _ = a.db.Exec(`UPDATE jobs SET status='running',message='' WHERE id=?`, job)
				continue
			}
			if e != nil {
				return e
			}
			pending := []metadataItem{}
			tx, e := a.db.BeginTx(ctx, nil)
			if e != nil {
				return e
			}
			for _, v := range page.Entries {
				if v.Dir {
					queue = append(queue, directory{v.Key, path.Join(current.folder, v.Name)})
					continue
				}
				if !isMedia(v.Name) {
					continue
				}
				var id, size, modified int64
				e = tx.QueryRow(`SELECT id,size,modified FROM videos WHERE source_id=? AND file_key=?`, s.ID, v.Key).Scan(&id, &size, &modified)
				fresh := e == sql.ErrNoRows
				if e != nil && !fresh {
					tx.Rollback()
					return e
				}
				result, e := tx.Exec(`INSERT INTO videos(source_id,file_key,playback_key,title,size,modified,indexed_at,seen_job,folder) VALUES(?,?,?,?,?,?,unixepoch(),?,?) ON CONFLICT(source_id,file_key) DO UPDATE SET playback_key=excluded.playback_key,title=excluded.title,size=excluded.size,modified=excluded.modified,seen_job=excluded.seen_job,folder=excluded.folder,available=1`, s.ID, v.Key, v.PlaybackKey, v.Name, v.Size, v.Modified, job, current.folder)
				if e != nil {
					tx.Rollback()
					return e
				}
				if fresh {
					id, _ = result.LastInsertId()
				}
				if fresh || size != v.Size || modified != v.Modified {
					_, _ = tx.Exec(`UPDATE videos SET duration=0,width=0,height=0,cover='' WHERE id=?`, id)
					pending = append(pending, metadataItem{id, v})
				}
				count++
			}
			if e = tx.Commit(); e != nil {
				return e
			}
			_, _ = a.db.Exec(`UPDATE jobs SET scanned=? WHERE id=?`, count, job)
			for _, v := range pending {
				if err = ctx.Err(); err != nil {
					return err
				}
				if e = a.metadata(ctx, d, s, v); e != nil {
					_, _ = a.db.Exec(`UPDATE jobs SET errors=errors+1 WHERE id=?`, job)
				}
			}
			if page.Next == "" {
				break
			}
			if page.Next == cursor {
				return errors.New("nonadvancing directory cursor")
			}
			cursor = page.Next
		}
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	_, err = a.db.ExecContext(ctx, `UPDATE videos SET available=0 WHERE source_id=? AND seen_job!=?`, s.ID, job)
	return err
}
func (a *App) metadata(ctx context.Context, d Storage, s Source, item metadataItem) error {
	select {
	case a.metadataGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-a.metadataGate }()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var title string
	_ = a.db.QueryRow(`SELECT title FROM videos WHERE id=?`, item.id).Scan(&title)
	mt := mediaType(title)
	name := itoa(item.id) + ".jpg"
	output := filepath.Join(a.cfg.DataDir, "covers", name)
	// Keep successful fields during metadata repair.
	var existingCover string
	_ = a.db.QueryRow(`SELECT cover FROM videos WHERE id=?`, item.id).Scan(&existingCover)
	cloudCover := existingCover != ""
	if s.Kind == "p115" && !cloudCover {
		cloudCover = a.downloadThumbnail(ctx, item.id, item.e.Thumb) == nil
	}
	stream, e := d.ResolvePlayback(ctx, item.e.Key, item.e.PlaybackKey, "MyTiktok/0.1", false)
	if e != nil {
		return e
	}
	if s.Kind == "p115" {
		// Fetch a bounded prefix plus the MP4 index, including an index at EOF.
		if mt == "video" {
			stream.Path, e = a.cloudPrefix(ctx, stream, item.e.Size)
		} else {
			stream.Path, e = a.cloudSmallAsset(ctx, stream, item.e.Size)
		}
		if e != nil {
			return e
		}
		defer os.Remove(stream.Path)
	}
	b, e := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration:stream=width,height,codec_type", "-of", "json", stream.Path).Output()
	if e != nil {
		return errors.New("metadata unavailable")
	}
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Width  int    `json:"width"`
			Height int    `json:"height"`
			Type   string `json:"codec_type"`
		} `json:"streams"`
	}
	if e = json.Unmarshal(b, &probe); e != nil {
		return e
	}
	duration, _ := strconv.ParseFloat(probe.Format.Duration, 64)
	width, height := 0, 0
	for _, s := range probe.Streams {
		if s.Type == "video" {
			width, height = s.Width, s.Height
			break
		}
	}
	_, e = a.db.Exec(`UPDATE videos SET duration=?,width=?,height=? WHERE id=?`, duration, width, height, item.id)
	if e != nil {
		return e
	}
	if cloudCover || mt == "audio" {
		return nil
	}
	seek := min(1.0, duration/4)
	if mt == "image" {
		seek = 0
	}
	e = exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-y", "-ss", fmt.Sprintf("%.3f", seek), "-i", stream.Path, "-frames:v", "1", "-vf", "scale=480:480:force_original_aspect_ratio=decrease", "-threads", "1", output).Run()
	if e != nil {
		return errors.New("cover unavailable")
	}
	_, e = a.db.Exec(`UPDATE videos SET cover=? WHERE id=?`, name, item.id)
	return e
}

func (a *App) downloadThumbnail(ctx context.Context, id int64, uri string) error {
	if uri == "" {
		return errors.New("no thumbnail")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", uri, nil)
	if e != nil {
		return e
	}
	resp, e := mediaHTTP().Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("thumbnail unavailable")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if e != nil || len(b) > 4<<20 {
		return errors.New("thumbnail too large")
	}
	ext := ""
	switch http.DetectContentType(b) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	default:
		return errors.New("invalid thumbnail")
	}
	name := itoa(id) + ext
	if e = os.WriteFile(filepath.Join(a.cfg.DataDir, "covers", name), b, 0600); e != nil {
		return e
	}
	_, e = a.db.Exec(`UPDATE videos SET cover=? WHERE id=?`, name, id)
	return e
}
