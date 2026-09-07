package app

import (
	"database/sql"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
)

type feedSession struct {
	IDs     []int64
	expires time.Time
	mode    string
}

const videoSelect = `SELECT v.id,v.source_id,s.kind,v.title,v.size,v.duration,v.width,v.height,v.cover,v.indexed_at,v.available,v.hidden,EXISTS(SELECT 1 FROM favorites f WHERE f.video_id=v.id),COALESCE(h.position,0),EXISTS(SELECT 1 FROM likes l WHERE l.video_id=v.id),s.name,v.folder,v.file_key,CASE WHEN v.modified>1000000000000 THEN v.modified/1000000000 ELSE v.modified END FROM videos v JOIN sources s ON s.id=v.source_id LEFT JOIN history h ON h.video_id=v.id `

func scanVideo(row interface{ Scan(...any) error }) (v Video, e error) {
	e = row.Scan(&v.ID, &v.SourceID, &v.Kind, &v.Title, &v.Size, &v.Duration, &v.Width, &v.Height, &v.Cover, &v.IndexedAt, &v.Available, &v.Hidden, &v.Favorite, &v.Position, &v.Liked, &v.SourceName, &v.Folder, &v.FileKey, &v.ModifiedAt)
	v.MediaType = mediaType(v.Title)
	if v.Cover != "" {
		v.Cover = "/api/videos/" + itoa(v.ID) + "/cover"
	}
	return
}
func (a *App) video(id int64) (Video, error) {
	return scanVideo(a.db.QueryRow(videoSelect+`WHERE v.id=?`, id))
}
func (a *App) feed(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cursor := q.Get("cursor")
	offset := 0
	var session *feedSession
	var id string
	if cursor != "" {
		parts := strings.Split(cursor, ":")
		if len(parts) != 2 {
			writeError(w, 400, "invalid_cursor", "分页参数无效")
			return
		}
		id = parts[0]
		offset = intParam(parts[1], -1)
		a.mu.Lock()
		session = a.feeds[id]
		a.mu.Unlock()
		if session == nil || time.Now().After(session.expires) {
			writeError(w, 410, "feed_expired", "浏览序列已过期，请刷新")
			return
		}
		if offset < 0 || offset > len(session.IDs) {
			writeError(w, 400, "invalid_cursor", "分页参数无效")
			return
		}
	} else {
		mode := q.Get("mode")
		if mode == "" {
			mode = "feed"
		}
		if mode != "likes" && mode != "feed" && mode != "favorites" && mode != "history" && mode != "hidden" {
			writeError(w, 400, "invalid_filter", "筛选无效")
			return
		}
		where := ` WHERE v.hidden=0 AND v.available=1`
		if mode == "hidden" {
			where = ` WHERE v.hidden=1`
		} else if mode == "likes" {
			where += ` AND EXISTS(SELECT 1 FROM likes l WHERE l.video_id=v.id)`
		} else if mode == "favorites" {
			where += ` AND EXISTS(SELECT 1 FROM favorites f WHERE f.video_id=v.id)`
		} else if mode == "history" {
			where += ` AND h.video_id IS NOT NULL`
		}
		args := []any{}
		if mt := q.Get("mediaType"); mt != "" {
			if mt != "video" && mt != "audio" && mt != "image" {
				writeError(w, 400, "invalid_filter", "媒体类型无效")
				return
			}
			where += " AND (" + mediaTypeSQL() + ")=?"
			args = append(args, mt)
		}
		if query := strings.TrimSpace(q.Get("q")); query != "" {
			if len(query) > 300 {
				writeError(w, 400, "invalid_filter", "搜索词过长")
				return
			}
			where += ` AND instr(lower(v.title),lower(?))>0`
			args = append(args, query)
		}
		if source := q.Get("sourceId"); source != "" {
			id := intParam(source, 0)
			if id == 0 {
				writeError(w, 400, "invalid_filter", "媒体源无效")
				return
			}
			where += ` AND v.source_id=?`
			args = append(args, id)
		}
		if q.Has("folder") {
			folder := q.Get("folder")
			if q.Get("recursive") == "1" {
				if folder != "" {
					where += ` AND (v.folder=? OR substr(v.folder,1,length(?))=?)`
					args = append(args, folder, folder+"/", folder+"/")
				}
			} else {
				where += ` AND v.folder=?`
				args = append(args, folder)
			}
		}
		switch q.Get("duration") {
		case "", "all":
		case "short":
			where += ` AND v.duration>0 AND v.duration<=60`
		case "medium":
			where += ` AND v.duration>60 AND v.duration<=300`
		case "long":
			where += ` AND v.duration>300`
		case "unknown":
			where += ` AND v.duration=0`
		default:
			writeError(w, 400, "invalid_filter", "时长筛选无效")
			return
		}
		switch q.Get("orientation") {
		case "", "all":
		case "portrait":
			where += ` AND v.height>v.width AND v.width>0`
		case "landscape":
			where += ` AND v.width>v.height AND v.height>0`
		case "square":
			where += ` AND v.width=v.height AND v.width>0`
		case "unknown":
			where += ` AND (v.width=0 OR v.height=0)`
		default:
			writeError(w, 400, "invalid_filter", "画幅筛选无效")
			return
		}
		kind := q.Get("kind")
		if kind == "local" || kind == "p115" {
			where += ` AND s.kind=?`
			args = append(args, kind)
		}
		order := ` ORDER BY v.indexed_at DESC,v.id DESC`
		if mode == "history" {
			order = ` ORDER BY h.watched_at DESC,v.id DESC`
		}
		switch q.Get("sort") {
		case "", "random", "recent":
		case "title":
			order = ` ORDER BY v.title COLLATE NOCASE,v.id`
		case "duration":
			order = ` ORDER BY v.duration DESC,v.id DESC`
		default:
			writeError(w, 400, "invalid_filter", "排序方式无效")
			return
		}
		rows, e := a.db.Query(`SELECT v.id FROM videos v JOIN sources s ON s.id=v.source_id LEFT JOIN history h ON h.video_id=v.id`+where+order, args...)
		if e != nil {
			fail(w, e)
			return
		}
		ids := []int64{}
		for rows.Next() {
			var n int64
			if e = rows.Scan(&n); e != nil {
				rows.Close()
				fail(w, e)
				return
			}
			ids = append(ids, n)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			fail(w, e)
			return
		}
		if mode == "feed" && (q.Get("sort") == "random" || q.Get("sort") == "") {
			rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
		}
		session = &feedSession{IDs: ids, expires: time.Now().Add(2 * time.Hour), mode: mode}
		id = token()
		a.mu.Lock()
		for k, v := range a.feeds {
			if time.Now().After(v.expires) {
				delete(a.feeds, k)
			}
		}
		if len(a.feeds) >= 32 {
			var oldest string
			var t time.Time
			for k, v := range a.feeds {
				if oldest == "" || v.expires.Before(t) {
					oldest = k
					t = v.expires
				}
			}
			delete(a.feeds, oldest)
		}
		a.feeds[id] = session
		a.mu.Unlock()
	}
	limit := min(max(intParam(q.Get("limit"), 20), 1), 50)
	out := []Video{}
	for offset < len(session.IDs) && len(out) < limit {
		v, e := a.video(session.IDs[offset])
		offset++
		if e == sql.ErrNoRows {
			continue
		}
		if e != nil {
			fail(w, e)
			return
		}
		if session.mode == "hidden" {
			if !v.Hidden {
				continue
			}
		} else {
			if v.Hidden || !v.Available {
				continue
			}
			if session.mode == "likes" && !v.Liked {
				continue
			}
			if session.mode == "favorites" && !v.Favorite {
				continue
			}
		}
		out = append(out, v)
	}
	next := ""
	if offset < len(session.IDs) {
		next = id + ":" + itoa(int64(offset))
	}
	jsonOK(w, map[string]any{"items": out, "next": next, "total": len(session.IDs)})
}
func (a *App) favorite(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Value bool `json:"value"`
	}
	if !decode(w, r, &b) {
		return
	}
	if _, e := a.video(pathID(r)); e != nil {
		fail(w, e)
		return
	}
	var e error
	if b.Value {
		_, e = a.db.Exec(`INSERT INTO favorites VALUES(?,unixepoch()) ON CONFLICT(video_id) DO NOTHING`, pathID(r))
	} else {
		_, e = a.db.Exec(`DELETE FROM favorites WHERE video_id=?`, pathID(r))
	}
	if e != nil {
		fail(w, e)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
func (a *App) hidden(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Value bool `json:"value"`
	}
	if !decode(w, r, &b) {
		return
	}
	if _, e := a.video(pathID(r)); e != nil {
		fail(w, e)
		return
	}
	_, e := a.db.Exec(`UPDATE videos SET hidden=? WHERE id=?`, b.Value, pathID(r))
	if e != nil {
		fail(w, e)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
func (a *App) history(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Position float64 `json:"position"`
	}
	if !decode(w, r, &b) {
		return
	}
	if b.Position < 0 || math.IsNaN(b.Position) || math.IsInf(b.Position, 0) {
		writeError(w, 400, "invalid_position", "播放进度无效")
		return
	}
	v, e := a.video(pathID(r))
	if e != nil {
		fail(w, e)
		return
	}
	if v.Duration > 0 {
		b.Position = min(b.Position, v.Duration)
	}
	_, e = a.db.Exec(`INSERT INTO history VALUES(?,?,?) ON CONFLICT(video_id) DO UPDATE SET position=excluded.position,watched_at=excluded.watched_at`, v.ID, b.Position, time.Now().UnixMilli())
	if e != nil {
		fail(w, e)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

func (a *App) like(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Value bool `json:"value"`
	}
	if !decode(w, r, &b) {
		return
	}
	if _, e := a.video(pathID(r)); e != nil {
		fail(w, e)
		return
	}
	var e error
	if b.Value {
		_, e = a.db.Exec(`INSERT INTO likes VALUES(?,unixepoch()) ON CONFLICT(video_id) DO NOTHING`, pathID(r))
	} else {
		_, e = a.db.Exec(`DELETE FROM likes WHERE video_id=?`, pathID(r))
	}
	if e != nil {
		fail(w, e)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
