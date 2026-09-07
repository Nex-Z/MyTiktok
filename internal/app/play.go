package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Provider URLs are not user inputs. Still reject private destinations and redirects.
func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil {
		return nil, e
	}
	ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
	if e != nil {
		return nil, e
	}
	for _, ip := range ips {
		if ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsUnspecified() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsLinkLocalMulticast() {
			return nil, errors.New("private upstream forbidden")
		}
	}
	var last error
	for _, ip := range ips {
		c, e := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if e == nil {
			return c, nil
		}
		last = e
	}
	if last == nil {
		last = errors.New("no upstream address")
	}
	return nil, last
}
func mediaHTTP() *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: publicDial, ResponseHeaderTimeout: 20 * time.Second, TLSHandshakeTimeout: 10 * time.Second, DisableKeepAlives: true}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
			return errors.New("invalid upstream")
		}
		return nil
	}}
}
func (a *App) play(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var sourceID int64
	var key string
	var available, hidden bool
	e := a.db.QueryRow(`SELECT source_id,file_key,available,hidden FROM videos WHERE id=?`, id).Scan(&sourceID, &key, &available, &hidden)
	if e != nil {
		fail(w, e)
		return
	}
	if !available || hidden {
		writeError(w, 404, "unavailable", "视频已隐藏或不可用")
		return
	}
	s, e := a.source(sourceID)
	if e != nil {
		fail(w, e)
		return
	}
	d, e := a.driver(s)
	if e != nil {
		fail(w, e)
		return
	}
	v, e := d.ResolvePlayback(r.Context(), key, r.UserAgent(), r.URL.Query().Get("refresh") == "1")
	if e != nil {
		fail(w, e)
		return
	}
	if v.Path != "" {
		f, e := os.Open(v.Path)
		if e != nil {
			fail(w, e)
			return
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil {
			fail(w, e)
			return
		}
		if !info.Mode().IsRegular() {
			writeError(w, 404, "unavailable", "不是可播放文件")
			return
		}
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
		return
	}
	u, e := url.Parse(v.URL)
	if e != nil || (u.Scheme != "https" && u.Scheme != "http") {
		fail(w, errCloud)
		return
	}
	if r.URL.Query().Get("proxy") != "1" {
		http.Redirect(w, r, v.URL, http.StatusFound)
		return
	}
	client := mediaHTTP()
	var resp *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		req, e := http.NewRequestWithContext(r.Context(), r.Method, v.URL, nil)
		if e != nil {
			fail(w, errCloud)
			return
		}
		req.Header = v.Headers.Clone()
		for _, k := range []string{"Range", "If-Range"} {
			if value := r.Header.Get(k); value != "" {
				req.Header.Set(k, value)
			}
		}
		resp, e = client.Do(req)
		if e != nil {
			writeError(w, 502, "playback_failed", "视频代理连接失败")
			return
		}
		if attempt == 0 && (resp.StatusCode == 401 || resp.StatusCode == 403) {
			resp.Body.Close()
			v, e = d.ResolvePlayback(r.Context(), key, r.UserAgent(), true)
			if e != nil {
				fail(w, e)
				return
			}
			continue
		}
		break
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 206 && resp.StatusCode != 416 {
		writeError(w, 502, "playback_failed", "云端视频暂时无法播放")
		return
	}
	copyStream(w, r, resp)
}
func copyStream(w http.ResponseWriter, r *http.Request, resp *http.Response) {
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method != "HEAD" {
		_, _ = io.Copy(w, resp.Body)
	}
}
func (a *App) cover(w http.ResponseWriter, r *http.Request) {
	var name string
	e := a.db.QueryRow(`SELECT cover FROM videos WHERE id=?`, pathID(r)).Scan(&name)
	if e != nil {
		fail(w, e)
		return
	}
	if name == "" || strings.ContainsAny(name, "/\\") {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(a.cfg.DataDir, "covers", name))
}
