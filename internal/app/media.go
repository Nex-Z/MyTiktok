package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var audioExtensions = []string{".mp3", ".m4a", ".aac", ".wav", ".flac", ".ogg", ".opus"}
var imageExtensions = []string{".jpg", ".jpeg", ".png", ".webp", ".bmp", ".avif"}

func mediaType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	for _, v := range audioExtensions {
		if ext == v {
			return "audio"
		}
	}
	for _, v := range imageExtensions {
		if ext == v {
			return "image"
		}
	}
	return "video"
}
func isMedia(name string) bool { return isVideo(name) || mediaType(name) != "video" }
func mediaTypeSQL() string {
	out := "CASE "
	for kind, extensions := range map[string][]string{"audio": audioExtensions, "image": imageExtensions} {
		for _, ext := range extensions {
			out += "WHEN lower(v.title) LIKE '%" + ext + "' THEN '" + kind + "' "
		}
	}
	return out + "ELSE 'video' END"
}

// Images and small audio files can be read in full for metadata, but this
// separate limit never changes the bounded video sampling policy.
func (a *App) cloudSmallAsset(ctx context.Context, p Playback, size int64) (string, error) {
	if size <= 0 || size > 8<<20 {
		return "", errors.New("asset metadata exceeds 8 MiB limit")
	}
	r, e := http.NewRequestWithContext(ctx, "GET", p.URL, nil)
	if e != nil {
		return "", e
	}
	r.Header = p.Headers.Clone()
	resp, e := mediaHTTP().Do(r)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New("asset unavailable")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if e != nil || len(b) > 8<<20 {
		return "", errors.New("asset too large")
	}
	f, e := os.CreateTemp(a.cfg.DataDir, "asset-*")
	if e != nil {
		return "", e
	}
	_, e = f.Write(b)
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		os.Remove(f.Name())
		return "", e
	}
	return f.Name(), nil
}
