package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	sdk "github.com/SheltonZhu/115driver/pkg/driver"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mockCloud(t *testing.T, f func(*http.Request) string) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(f(r))), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = old })
}
func TestP115PaginationAndCredentialIsolation(t *testing.T) {
	requests := 0
	mockCloud(t, func(r *http.Request) string {
		requests++
		offset := r.URL.Query().Get("offset")
		if offset == "0" {
			return `{"state":true,"cid":"0","count":2,"offset":0,"data":[{"fid":"1","cid":"0","n":"one.mp4","s":10,"pc":"pick1"}]}`
		}
		if offset != "1" {
			t.Errorf("bad offset %s", offset)
		}
		return `{"state":true,"cid":"0","count":2,"offset":1,"data":[{"fid":"2","cid":"0","n":"two.mp4","s":20,"pc":"pick2"}]}`
	})
	p := newP115("UID=1; CID=2; SEID=secret")
	p.interval = 0
	page, e := p.List(context.Background(), "0", "")
	if e != nil {
		t.Fatal(e)
	}
	if page.Next != "1" || page.Entries[0].Key != "1" || page.Entries[0].PlaybackKey != "pick1" {
		t.Fatalf("bad first page %+v", page)
	}
	page, e = p.List(context.Background(), "0", page.Next)
	if e != nil || page.Next != "" || len(page.Entries) != 1 || requests != 2 {
		t.Fatalf("bad pagination %+v %v", page, e)
	}
	b, _ := json.Marshal(page)
	if strings.Contains(string(b), "secret") {
		t.Fatal("credential leak")
	}
}
func TestP115CanceledAndCooling(t *testing.T) {
	p := newP115("")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.last = time.Now()
	_, e := p.List(ctx, "0", "")
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	p.coolUntil = time.Now().Add(time.Minute)
	_, e = p.List(context.Background(), "0", "")
	if !errors.Is(e, errRate) {
		t.Fatal(e)
	}
}
func TestP115ErrorsRedacted(t *testing.T) {
	for _, err := range []error{sdk.ErrNotLogin, sdk.ErrCredentialInvalid, sdk.ErrSessionExited} {
		if !errors.Is(cloudError(fmt.Errorf("wrapped: %w", err)), errAuth) {
			t.Fatal(err)
		}
	}
	e := cloudError(errors.New("429 UID=secret"))
	if !errors.Is(e, errRate) || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
	e = cloudError(errors.New("upstream body SEID=secret"))
	if e != errCloud {
		t.Fatal(e)
	}
}
func TestExpiredQR(t *testing.T) {
	a := testApp(t)
	a.qrs["expired"] = &qrSession{expires: time.Now().Add(-time.Second)}
	w := request(t, a, "GET", "/api/p115/qr/expired", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "expired") {
		t.Fatal(w.Body.String())
	}
}
func TestP115CredentialFailureDoesNotPrune(t *testing.T) {
	a := testApp(t)
	mustExec(t, a, `INSERT INTO sources VALUES(1,'cloud','p115','0',6,0)`)
	mustExec(t, a, `INSERT INTO videos(source_id,file_key,title,size,modified,indexed_at) VALUES(1,'1','clip',1,1,1)`)
	s, _ := a.source(1)
	if err := a.scan(context.Background(), s, 1); !errors.Is(err, errAuth) {
		t.Fatal(err)
	}
	v, _ := a.video(1)
	if !v.Available {
		t.Fatal("credentials failure removed index")
	}
}

func TestP115PlaybackCacheExpiryAndRefresh(t *testing.T) {
	mockCloud(t, func(r *http.Request) string { return `{"state":false,"errno":99}` })
	p := newP115("")
	p.cache["1\nBrowser"] = Playback{URL: "https://example.invalid/signed", Expires: time.Now().Add(time.Minute)}
	v, err := p.ResolvePlayback(context.Background(), "1", "", "Browser", false)
	if err != nil || v.URL == "" {
		t.Fatalf("cache miss: %v", err)
	}
	if _, err = p.ResolvePlayback(context.Background(), "1", "", "Browser", true); !errors.Is(err, errAuth) {
		t.Fatalf("refresh did not resolve upstream: %v", err)
	}
	p.cache["1\nBrowser"] = Playback{URL: "https://example.invalid/expired", Expires: time.Now().Add(-time.Minute)}
	if _, err = p.ResolvePlayback(context.Background(), "1", "", "Browser", false); !errors.Is(err, errAuth) {
		t.Fatalf("expired URL reused: %v", err)
	}
}

func TestP115PlaybackKeySkipsFileLookup(t *testing.T) {
	requests := 0
	mockCloud(t, func(r *http.Request) string {
		requests++
		if strings.Contains(r.URL.Path, "get_info") {
			t.Fatal("playback key unexpectedly performed file lookup")
		}
		return `{"state":false,"errno":99}`
	})
	p := newP115("")
	_, _ = p.ResolvePlayback(context.Background(), "1", "pick1", "Browser", false)
	if requests != 1 {
		t.Fatalf("expected one download URL request, got %d", requests)
	}
}

func TestP115HTTPRateLimitBeforeJSONDecode(t *testing.T) {
	for _, status := range []int{405, 429} {
		base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("<html>blocked</html>"))}, nil
		})
		transport := contextTransport{ctx: context.Background(), base: base}
		r, _ := http.NewRequest("GET", "https://example.invalid", nil)
		_, err := transport.RoundTrip(r)
		if !errors.Is(cloudError(err), errRate) {
			t.Fatalf("status %d: %v", status, err)
		}
	}
}
