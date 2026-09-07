package app

import (
	"context"
	"errors"
	sdk "github.com/SheltonZhu/115driver/pkg/driver"
	"net/http"
	"time"
)

// One account-wide gate includes the SDK's initialization and rapid-upload calls.
type paced115 struct{ p *P115 }

func (t paced115) RoundTrip(r *http.Request) (*http.Response, error) {
	p := t.p
	select {
	case p.requestGate <- struct{}{}:
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}
	defer func() { <-p.requestGate }()
	if time.Now().Before(p.requestCool) {
		return nil, errRate
	}
	if e := sleep(r.Context(), max(time.Until(p.requestLast.Add(p.interval)), 0)); e != nil {
		return nil, e
	}
	p.requestLast = time.Now()
	resp, e := (contextTransport{r.Context(), http.DefaultTransport}).RoundTrip(r)
	if errors.Is(e, errRate) {
		p.requestCool = time.Now().Add(10 * time.Minute)
	}
	return resp, e
}
func (p *P115) client(ctx context.Context) *sdk.Pan115Client {
	return cloudClientTransport(p.cookie, contextTransport{ctx, paced115{p}})
}
