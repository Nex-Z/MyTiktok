package app

import (
	"context"
	"encoding/base64"
	"errors"
	sdk "github.com/SheltonZhu/115driver/pkg/driver"
	"net/http"
	"strings"
	"sync"
	"time"
)

var errAuth = errors.New("115 登录已失效，请重新连接")
var errRate = errors.New("115 请求受限，请稍后重试")
var errCloud = errors.New("115 请求失败，请检查连接后重试")

type contextTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t contextTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(r.Clone(t.ctx))
	if err != nil {
		return resp, err
	}
	// WAF responses can be HTML; classify status before SDK JSON decoding.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusMethodNotAllowed {
		resp.Body.Close()
		return nil, errRate
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return nil, errAuth
	}
	return resp, nil
}
func cloudClient(ctx context.Context, cookie string) *sdk.Pan115Client {
	return cloudClientTransport(cookie, contextTransport{ctx, http.DefaultTransport})
}
func cloudClientTransport(cookie string, transport http.RoundTripper) *sdk.Pan115Client {
	c := sdk.New(sdk.WithClient(&http.Client{Timeout: 30 * time.Second, Transport: transport}), sdk.UA(sdk.UA115Browser))
	if cookie != "" {
		cr := &sdk.Credential{}
		_ = cr.FromCookie(cookie)
		c.ImportCredential(cr)
	}
	return c
}
func cloudError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errRate) {
		return errRate
	}
	if errors.Is(err, errAuth) {
		return errAuth
	}
	if errors.Is(err, sdk.ErrCredentialInvalid) || errors.Is(err, sdk.ErrSessionExited) || errors.Is(err, sdk.ErrNotLogin) || errors.Is(err, sdk.ErrDoesLoggedOut) || errors.Is(err, sdk.ErrBadCookie) {
		return errAuth
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "429") || strings.Contains(s, "405") {
		return errRate
	}
	if strings.Contains(s, "login") || strings.Contains(s, "登录") || strings.Contains(s, "cookie") || strings.Contains(s, "401") {
		return errAuth
	}
	return errCloud
}

type P115 struct {
	requestGate chan struct{}
	requestLast time.Time
	requestCool time.Time
	cookie      string
	gate        chan struct{}
	mu          sync.Mutex
	last        time.Time
	coolUntil   time.Time
	cache       map[string]Playback
	interval    time.Duration
}

func newP115(cookie string) *P115 {
	return &P115{cookie: cookie, gate: make(chan struct{}, 1), requestGate: make(chan struct{}, 1), cache: map[string]Playback{}, interval: 2 * time.Second}
}
func (p *P115) List(ctx context.Context, key, cursor string) (Page, error) {
	select {
	case p.gate <- struct{}{}:
	case <-ctx.Done():
		return Page{}, ctx.Err()
	}
	defer func() { <-p.gate }()
	if time.Now().Before(p.coolUntil) {
		return Page{}, errRate
	}
	delay := time.Until(p.last.Add(p.interval))
	if delay > 0 {
		if err := sleep(ctx, delay); err != nil {
			return Page{}, err
		}
	}
	p.last = time.Now()
	c := p.client(ctx)
	offset := int64(intParam(cursor, 0))
	opts := sdk.DefaultListOptions()
	result, err := sdk.GetFiles(c.NewRequest().SetContext(ctx).ForceContentType("application/json;charset=UTF-8"), key, sdk.WithApiURL(opts.ApiURLs[0]), sdk.WithLimit(256), sdk.WithOffset(offset))
	if err != nil {
		err = cloudError(err)
		if errors.Is(err, errRate) {
			p.coolUntil = time.Now().Add(10 * time.Minute)
		}
		return Page{}, err
	}
	if string(result.CategoryID) != key {
		return Page{}, errCloud
	}
	out := Page{Entries: []Entry{}}
	for i := range result.Files {
		f := (&sdk.File{}).From(&result.Files[i])
		out.Entries = append(out.Entries, Entry{Key: f.FileID, Name: f.Name, Dir: f.IsDirectory, Size: f.Size, Modified: f.UpdateTime.Unix(), Thumb: f.ThumbURL})
	}
	next := offset + int64(len(result.Files))
	if next < int64(result.Count) {
		if len(result.Files) == 0 {
			return Page{}, errCloud
		}
		out.Next = itoa(next)
	}
	return out, nil
}
func (p *P115) Stat(ctx context.Context, key string) (Entry, error) {
	f, err := p.client(ctx).GetFile(key)
	if err != nil {
		return Entry{}, cloudError(err)
	}
	if f == nil {
		return Entry{}, errCloud
	}
	return Entry{Key: f.FileID, Name: f.Name, Dir: f.IsDirectory, Size: f.Size, Modified: f.UpdateTime.Unix(), Thumb: f.ThumbURL}, nil
}
func (p *P115) ResolvePlayback(ctx context.Context, key, ua string, refresh bool) (Playback, error) {
	cacheKey := key + "\n" + ua
	p.mu.Lock()
	v, ok := p.cache[cacheKey]
	p.mu.Unlock()
	if ok && !refresh && time.Now().Before(v.Expires) {
		return v, nil
	}
	c := p.client(ctx)
	f, err := c.GetFile(key)
	if err != nil {
		return Playback{}, cloudError(err)
	}
	if f == nil {
		return Playback{}, errCloud
	}
	info, err := c.DownloadWithUA(f.PickCode, ua)
	if err != nil {
		return Playback{}, cloudError(err)
	}
	if info == nil || info.Url.Url == "" {
		return Playback{}, errCloud
	}
	// Preserve CDN-specific cookies, but never forward account credentials.
	headers := http.Header{"User-Agent": {ua}, "Referer": {"https://115.com/"}}
	if info.Header != nil {
		cookies := (&http.Request{Header: info.Header}).Cookies()
		for _, cookie := range cookies {
			switch strings.ToUpper(cookie.Name) {
			case "UID", "CID", "SEID", "KID":
				continue
			}
			headers.Add("Cookie", cookie.Name+"="+cookie.Value)
		}
	}
	v = Playback{URL: info.Url.Url, Headers: headers, Expires: time.Now().Add(5 * time.Minute)}
	p.mu.Lock()
	if len(p.cache) > 256 {
		clear(p.cache)
	}
	p.cache[cacheKey] = v
	p.mu.Unlock()
	return v, nil
}
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type qrSession struct {
	session *sdk.QRCodeSession
	expires time.Time
	mu      sync.Mutex
	state   string
}

func (a *App) saveCookie(ctx context.Context, cookie string) error {
	cr := &sdk.Credential{}
	if cr.FromCookie(cookie) != nil {
		return errAuth
	}
	if err := cloudClient(ctx, cookie).LoginCheck(); err != nil {
		return cloudError(err)
	}
	if _, err := a.db.Exec(`INSERT INTO settings(key,value) VALUES('p115_cookie',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, cookie); err != nil {
		return err
	}
	a.mu.Lock()
	a.cloud = newP115(cookie)
	a.mu.Unlock()
	return nil
}
func (a *App) qrStart(w http.ResponseWriter, r *http.Request) {
	s, err := cloudClient(r.Context(), "").QRCodeStart()
	if err != nil {
		fail(w, cloudError(err))
		return
	}
	if s == nil {
		fail(w, errCloud)
		return
	}
	png, err := s.QRCode()
	if err != nil {
		fail(w, err)
		return
	}
	id := token()
	a.mu.Lock()
	for k, v := range a.qrs {
		if time.Now().After(v.expires) {
			delete(a.qrs, k)
		}
	}
	if len(a.qrs) >= 16 {
		a.mu.Unlock()
		writeError(w, 429, "rate_limited", "扫码会话过多，请稍后重试")
		return
	}
	a.qrs[id] = &qrSession{session: s, expires: time.Now().Add(3 * time.Minute), state: "waiting"}
	a.mu.Unlock()
	jsonOK(w, map[string]any{"id": id, "image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)})
}
func (a *App) qrPoll(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	q := a.qrs[r.PathValue("id")]
	a.mu.Unlock()
	if q == nil {
		jsonOK(w, map[string]string{"state": "expired"})
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.state == "success" || q.state == "canceled" {
		jsonOK(w, map[string]string{"state": q.state})
		return
	}
	if time.Now().After(q.expires) {
		jsonOK(w, map[string]string{"state": "expired"})
		return
	}
	c := cloudClient(r.Context(), "")
	s, err := c.QRCodeStatus(q.session)
	if errors.Is(err, sdk.ErrQrcodeExpired) {
		jsonOK(w, map[string]string{"state": "expired"})
		return
	}
	if err != nil {
		fail(w, cloudError(err))
		return
	}
	if s == nil {
		fail(w, errCloud)
		return
	}
	switch {
	case s.IsAllowed():
		cr, e := c.QRCodeLoginWithApp(q.session, sdk.LoginAppWeb)
		if e != nil {
			fail(w, cloudError(e))
			return
		}
		if cr == nil {
			fail(w, errCloud)
			return
		}
		if e = a.saveCookie(r.Context(), cr.Cookie()); e != nil {
			fail(w, e)
			return
		}
		q.state = "success"
	case s.IsScanned():
		q.state = "scanned"
	case s.IsCanceled():
		q.state = "canceled"
	case s.IsExpired():
		q.state = "expired"
	default:
		q.state = "waiting"
	}
	jsonOK(w, map[string]string{"state": q.state})
}
