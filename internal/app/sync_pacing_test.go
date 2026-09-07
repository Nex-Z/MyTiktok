package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSyncAccountRequestPacingAndRateStop(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	var times []time.Time
	status := 200
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		times = append(times, time.Now())
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}}, nil
	})
	p := newP115("")
	p.interval = 20 * time.Millisecond
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("GET", "https://example.test", nil)
		resp, e := (paced115{p}).RoundTrip(req)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
	}
	for i := 1; i < len(times); i++ {
		if times[i].Sub(times[i-1]) < p.interval {
			t.Fatal("requests not paced")
		}
	}
	status = 429
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	if _, e := (paced115{p}).RoundTrip(req); !errors.Is(e, errRate) {
		t.Fatal(e)
	}
	count := len(times)
	if _, e := (paced115{p}).RoundTrip(req); !errors.Is(e, errRate) {
		t.Fatal(e)
	}
	if len(times) != count {
		t.Fatal("request sent during cooldown")
	}
}
func TestSyncRequestWaitIsCancelable(t *testing.T) {
	p := newP115("")
	p.requestLast = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://example.test", nil)
	if _, e := (paced115{p}).RoundTrip(req); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
