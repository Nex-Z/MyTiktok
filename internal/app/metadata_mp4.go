package app

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Fetch only the prefix and MP4 index. Original sample offsets in the prefix
// remain valid when its truncated mdat box is closed before appending moov.
func (a *App) cloudPrefix(ctx context.Context, v Playback, size int64) (string, error) {
	return a.cloudMP4Sample(ctx, v, size, mediaHTTP())
}
func (a *App) cloudMP4Sample(ctx context.Context, v Playback, size int64, client *http.Client) (string, error) {
	budget := min(int64(16<<20), size*3/4)
	read := func(start, n int64) ([]byte, error) {
		if n <= 0 || n > budget || start < 0 || start > size-n {
			return nil, errors.New("metadata byte budget exceeded")
		}
		budget -= n
		req, e := http.NewRequestWithContext(ctx, "GET", v.URL, nil)
		if e != nil {
			return nil, e
		}
		req.Header = v.Headers.Clone()
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, start+n-1))
		req.Header.Set("Accept-Encoding", "identity")
		resp, e := client.Do(req)
		if e != nil {
			return nil, e
		}
		defer resp.Body.Close()
		if resp.StatusCode != 206 {
			return nil, errors.New("metadata range unsupported")
		}
		var first, last, total int64
		if _, e = fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &first, &last, &total); e != nil || first != start || last != start+n-1 || total != size {
			return nil, errors.New("invalid metadata range")
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, n))
		if e != nil {
			return nil, e
		}
		if int64(len(b)) != n {
			return nil, io.ErrUnexpectedEOF
		}
		return b, nil
	}
	prefix, e := read(0, min(int64(8<<20), size/2))
	if e != nil {
		return "", e
	}
	for at, boxes := int64(0), 0; at < size && boxes < 64; boxes++ {
		var header []byte
		if at+16 <= int64(len(prefix)) {
			header = prefix[at : at+16]
		} else {
			header, e = read(at, min(int64(16), size-at))
			if e != nil {
				return "", e
			}
		}
		if len(header) < 8 {
			return "", errors.New("invalid MP4 box")
		}
		n := int64(binary.BigEndian.Uint32(header[:4]))
		kind := string(header[4:8])
		headerSize := int64(8)
		if n == 1 {
			if len(header) < 16 {
				return "", errors.New("invalid extended box")
			}
			u := binary.BigEndian.Uint64(header[8:16])
			if u > uint64(size) {
				return "", errors.New("invalid box size")
			}
			n = int64(u)
			headerSize = 16
		} else if n == 0 {
			n = size - at
		}
		if n < headerSize || n > size-at {
			return "", errors.New("invalid MP4 box length")
		}
		if kind == "moov" {
			if at+n > int64(len(prefix)) {
				index, e := read(at, n)
				if e != nil {
					return "", e
				}
				if at < int64(len(prefix)) {
					prefix = prefix[:at]
				}
				prefix = append(prefix, index...)
			}
			tmp, e := os.CreateTemp(a.cfg.DataDir, "probe-*.mp4")
			if e != nil {
				return "", e
			}
			_, e = tmp.Write(prefix)
			closeErr := tmp.Close()
			if e == nil {
				e = closeErr
			}
			if e != nil {
				os.Remove(tmp.Name())
				return "", e
			}
			return tmp.Name(), nil
		}
		if at < int64(len(prefix)) && at+n > int64(len(prefix)) {
			if kind != "mdat" && kind != "free" && kind != "skip" {
				return "", errors.New("unsupported partial MP4 box")
			}
			clipped := int64(len(prefix)) - at
			if clipped < headerSize {
				return "", errors.New("incomplete MP4 header")
			}
			if headerSize == 16 {
				binary.BigEndian.PutUint64(prefix[at+8:at+16], uint64(clipped))
			} else {
				binary.BigEndian.PutUint32(prefix[at:at+4], uint32(clipped))
			}
		}
		at += n
	}
	return "", errors.New("MP4 index not found within request budget")
}
