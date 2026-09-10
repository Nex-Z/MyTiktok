package app

import (
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
)

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS schema_version(version INTEGER NOT NULL);
INSERT INTO schema_version SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM schema_version);
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sources(
 id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('local','p115')),
 root TEXT NOT NULL, interval_hours INTEGER NOT NULL DEFAULT 6, last_scan INTEGER NOT NULL DEFAULT 0,
 UNIQUE(kind,root));
CREATE TABLE IF NOT EXISTS videos(
 id INTEGER PRIMARY KEY AUTOINCREMENT, source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
 file_key TEXT NOT NULL, title TEXT NOT NULL, size INTEGER NOT NULL, modified INTEGER NOT NULL,
 duration REAL NOT NULL DEFAULT 0,width INTEGER NOT NULL DEFAULT 0,height INTEGER NOT NULL DEFAULT 0,
 cover TEXT NOT NULL DEFAULT '',indexed_at INTEGER NOT NULL, seen_job INTEGER NOT NULL DEFAULT 0,
 available INTEGER NOT NULL DEFAULT 1,hidden INTEGER NOT NULL DEFAULT 0,
 UNIQUE(source_id,file_key));
CREATE INDEX IF NOT EXISTS videos_feed ON videos(hidden,available,indexed_at DESC,id DESC);
CREATE TABLE IF NOT EXISTS likes(video_id INTEGER PRIMARY KEY REFERENCES videos(id) ON DELETE CASCADE,created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS favorites(video_id INTEGER PRIMARY KEY REFERENCES videos(id) ON DELETE CASCADE,created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS history(video_id INTEGER PRIMARY KEY REFERENCES videos(id) ON DELETE CASCADE,position REAL NOT NULL,watched_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS jobs(id INTEGER PRIMARY KEY AUTOINCREMENT,source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
 status TEXT NOT NULL,scanned INTEGER NOT NULL DEFAULT 0,errors INTEGER NOT NULL DEFAULT 0,message TEXT NOT NULL DEFAULT '',
 started_at INTEGER NOT NULL,finished_at INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS history_recent ON history(watched_at DESC);
`

func openDB(dir string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Join(dir, "covers"), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "library.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	var version int
	if err = db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil || version < 1 || version > 3 {
		db.Close()
		return nil, fmt.Errorf("unsupported database schema version %d", version)
	}
	if version == 1 {
		if err = migrateFolders(db); err != nil {
			db.Close()
			return nil, err
		}
		version = 2
	}
	if version == 2 {
		if err = migratePlaybackKeys(db); err != nil {
			db.Close()
			return nil, err
		}
	}
	_, err = db.Exec(`UPDATE jobs SET status='interrupted',message='服务重启，任务已中断',finished_at=unixepoch() WHERE status IN ('running','cooling','queued')`)
	return db, err
}

func migratePlaybackKeys(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`ALTER TABLE videos ADD COLUMN playback_key TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE schema_version SET version=3`); err != nil {
		return err
	}
	return tx.Commit()
}

func migrateFolders(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`ALTER TABLE videos ADD COLUMN folder TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT v.id,v.file_key FROM videos v JOIN sources s ON s.id=v.source_id WHERE s.kind='local'`)
	if err != nil {
		return err
	}
	type item struct {
		id     int64
		folder string
	}
	items := []item{}
	for rows.Next() {
		var id int64
		var key string
		if err = rows.Scan(&id, &key); err != nil {
			rows.Close()
			return err
		}
		key = strings.ReplaceAll(key, "\\", "/")
		if n := strings.LastIndex(key, "/"); n >= 0 {
			items = append(items, item{id, key[:n]})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range items {
		if _, err = tx.Exec(`UPDATE videos SET folder=? WHERE id=?`, v.folder, v.id); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS videos_folder ON videos(source_id,folder);UPDATE schema_version SET version=2`); err != nil {
		return err
	}
	return tx.Commit()
}

type Source struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Root     string `json:"root"`
	Interval int    `json:"intervalHours"`
	LastScan int64  `json:"lastScan"`
}
type Video struct {
	MediaType  string  `json:"mediaType"`
	Liked      bool    `json:"liked"`
	SourceName string  `json:"sourceName"`
	Folder     string  `json:"folder"`
	FileKey    string  `json:"fileKey"`
	ModifiedAt int64   `json:"modifiedAt"`
	ID         int64   `json:"id"`
	SourceID   int64   `json:"sourceId"`
	Kind       string  `json:"kind"`
	Title      string  `json:"title"`
	Size       int64   `json:"size"`
	Duration   float64 `json:"duration"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	Cover      string  `json:"cover"`
	IndexedAt  int64   `json:"indexedAt"`
	Available  bool    `json:"available"`
	Hidden     bool    `json:"hidden"`
	Favorite   bool    `json:"favorite"`
	Position   float64 `json:"position"`
}
type Job struct {
	ID         int64  `json:"id"`
	SourceID   int64  `json:"sourceId"`
	Status     string `json:"status"`
	Scanned    int    `json:"scanned"`
	Errors     int    `json:"errors"`
	Message    string `json:"message"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
}

func (a *App) source(id int64) (s Source, err error) {
	err = a.db.QueryRow(`SELECT id,name,kind,root,interval_hours,last_scan FROM sources WHERE id=?`, id).Scan(&s.ID, &s.Name, &s.Kind, &s.Root, &s.Interval, &s.LastScan)
	return
}
func (a *App) sources() ([]Source, error) {
	rows, err := a.db.Query(`SELECT id,name,kind,root,interval_hours,last_scan FROM sources ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		var s Source
		if err = rows.Scan(&s.ID, &s.Name, &s.Kind, &s.Root, &s.Interval, &s.LastScan); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
