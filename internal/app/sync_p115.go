package app

import (
	"context"
	sdk "github.com/SheltonZhu/115driver/pkg/driver"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"io"
	"net/http"
	"os"
	"time"
)

// Upload uses a cancelable reader for hashing and an explicit context for OSS.
// Avoid the SDK convenience method, whose OSS request has no caller context.
type syncReader struct {
	ctx context.Context
	f   *os.File
}

func (r syncReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.f.Read(b)
}
func (r syncReader) Seek(n int64, w int) (int64, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.f.Seek(n, w)
}
func (p *P115) Mkdir(ctx context.Context, parent, name string) (string, error) {
	select {
	case p.gate <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-p.gate }()
	if e := sleep(ctx, max(time.Until(p.last.Add(p.interval)), 0)); e != nil {
		return "", e
	}
	p.last = time.Now()
	id, e := cloudClient(ctx, p.cookie).Mkdir(parent, name)
	return id, cloudError(e)
}
func (p *P115) Upload(ctx context.Context, parent, name string, f *os.File) error {
	c := cloudClient(ctx, p.cookie)
	before, e := f.Stat()
	if e != nil {
		return e
	}
	if ok, e := c.UploadAvailable(); e != nil {
		return cloudError(e)
	} else if !ok {
		return errCloud
	}
	if before.Size() > c.UploadMetaInfo.SizeLimit {
		return sdk.ErrUploadTooLarge
	}
	r := syncReader{ctx, f}
	if _, e := r.Seek(0, io.SeekStart); e != nil {
		return e
	}
	digest, e := c.GetDigestResult(r)
	if e != nil {
		return cloudError(e)
	}
	after, e := f.Stat()
	if e != nil {
		return e
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return errCloud
	}
	init, e := c.RapidUpload(digest.Size, name, parent, digest.PreID, digest.QuickID, r)
	if e != nil {
		return cloudError(e)
	}
	ok, e := init.Ok()
	if e != nil {
		return cloudError(e)
	}
	if ok {
		return nil
	}
	token, e := c.GetOSSToken()
	if e != nil {
		return cloudError(e)
	}
	client, e := oss.New(c.GetOSSEndpoint(false), token.AccessKeyID, token.AccessKeySecret, oss.HTTPClient(&http.Client{Transport: contextTransport{ctx, http.DefaultTransport}}))
	if e != nil {
		return cloudError(e)
	}
	bucket, e := client.Bucket(init.Bucket)
	if e != nil {
		return cloudError(e)
	}
	if _, e = r.Seek(0, io.SeekStart); e != nil {
		return e
	}
	opts := sdk.OssOption(&init.UploadOSSParams, token)
	opts = append(opts, oss.WithContext(ctx))
	if e = bucket.PutObject(init.Object, r, opts...); e != nil {
		return cloudError(e)
	}
	// Confirm the remote name and size before recording success.
	for attempt := 0; attempt < 3; attempt++ {
		cursor := ""
		for {
			page, e := p.List(ctx, parent, cursor)
			if e != nil {
				return e
			}
			for _, v := range page.Entries {
				if !v.Dir && v.Name == name && v.Size == digest.Size {
					return nil
				}
			}
			if page.Next == "" {
				break
			}
			cursor = page.Next
		}
		if e = sleep(ctx, 2*time.Second); e != nil {
			return e
		}
	}
	return errCloud
}
