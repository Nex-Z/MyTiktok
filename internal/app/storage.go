package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Entry struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Thumb    string `json:"-"`
}
type Page struct {
	Entries []Entry `json:"entries"`
	Next    string  `json:"next"`
}
type Playback struct {
	Path    string
	URL     string
	Headers http.Header
	Expires time.Time
}
type Storage interface {
	List(context.Context, string, string) (Page, error)
	Stat(context.Context, string) (Entry, error)
	ResolvePlayback(context.Context, string, string, bool) (Playback, error)
}

// safePath resolves symlinks before enforcing the configured root boundary.
func safePath(root, key string) (string, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(key) {
		return "", errors.New("absolute media key forbidden")
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(key)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("outside media root")
	}
	return target, nil
}

type Local struct{ Root string }

func (l *Local) List(ctx context.Context, key, cursor string) (Page, error) {
	p, err := safePath(l.Root, key)
	if err != nil {
		return Page{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		return Page{}, err
	}
	defer f.Close()
	// Cursor is an entry offset; ReadDir(n) bounds per-page memory.
	skip := intParam(cursor, 0)
	for skip > 0 {
		n := min(skip, 256)
		v, e := f.ReadDir(n)
		skip -= len(v)
		if e != nil && e != io.EOF {
			return Page{}, e
		}
		if e == io.EOF {
			return Page{Entries: []Entry{}}, nil
		}
	}
	entries, err := f.ReadDir(256)
	if err != nil && err != io.EOF {
		return Page{}, err
	}
	out := Page{Entries: []Entry{}}
	for _, d := range entries {
		if err = ctx.Err(); err != nil {
			return Page{}, err
		}
		if d.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, e := d.Info()
		if e != nil {
			return Page{}, e
		}
		out.Entries = append(out.Entries, Entry{Key: filepath.ToSlash(filepath.Join(key, d.Name())), Name: d.Name(), Dir: d.IsDir(), Size: info.Size(), Modified: info.ModTime().UnixNano()})
	}
	if len(entries) == 256 {
		out.Next = itoa(int64(intParam(cursor, 0) + 256))
	}
	return out, nil
}
func (l *Local) Stat(ctx context.Context, key string) (Entry, error) {
	p, err := safePath(l.Root, key)
	if err != nil {
		return Entry{}, err
	}
	i, err := os.Stat(p)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Key: key, Name: i.Name(), Dir: i.IsDir(), Size: i.Size(), Modified: i.ModTime().UnixNano()}, ctx.Err()
}
func (l *Local) ResolvePlayback(ctx context.Context, key, ua string, refresh bool) (Playback, error) {
	p, err := safePath(l.Root, key)
	return Playback{Path: p}, err
}
func isVideo(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".m4v", ".mov", ".webm", ".mkv", ".avi":
		return true
	}
	return false
}
