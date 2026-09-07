package app

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestMP4TailIndexSample(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("ffmpeg unavailable")
	}
	a := testApp(t)
	file := filepath.Join(t.TempDir(), "tail.mp4")
	if b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24", "-t", "4", "-c:v", "libx264", file).CombinedOutput(); e != nil {
		t.Fatalf("%v %s", e, b)
	}
	data, _ := os.ReadFile(file)
	tail := false
	for at := 0; at+8 < len(data); {
		n := int(binary.BigEndian.Uint32(data[at : at+4]))
		if string(data[at+4:at+8]) == "moov" {
			tail = at > len(data)/2
			break
		}
		if n < 8 {
			break
		}
		at += n
	}
	if !tail {
		t.Fatal("fixture must have tail index")
	}
	var sent atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, _ := os.Open(file)
		defer f.Close()
		info, _ := f.Stat()
		first, last := int64(0), int64(0)
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &first, &last)
		sent.Add(last - first + 1)
		http.ServeContent(w, r, "tail.mp4", info.ModTime(), f)
	}))
	defer srv.Close()
	p, e := a.cloudMP4Sample(context.Background(), Playback{URL: srv.URL, Headers: http.Header{}}, int64(len(data)), srv.Client())
	if e != nil {
		t.Fatal(e)
	}
	defer os.Remove(p)
	if sent.Load() >= int64(len(data)) {
		t.Fatal("entire original downloaded")
	}
	b, e := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "json", p).Output()
	if e != nil {
		t.Fatal(e)
	}
	var result struct{ Format struct{ Duration string } }
	json.Unmarshal(b, &result)
	if result.Format.Duration != "4.000000" {
		t.Fatalf("%s", b)
	}
	if b, e = exec.Command("ffmpeg", "-v", "error", "-ss", "0.2", "-i", p, "-frames:v", "1", filepath.Join(t.TempDir(), "cover.jpg")).CombinedOutput(); e != nil {
		t.Fatalf("cover failed: %v %s", e, b)
	}
}
func TestMP4RejectsIgnoredRange(t *testing.T) {
	a := testApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	if _, e := a.cloudMP4Sample(context.Background(), Playback{URL: srv.URL, Headers: http.Header{}}, 10000, srv.Client()); e == nil {
		t.Fatal("accepted full response")
	}
}

func TestMetadataRepairPreservesExistingFieldsAndAvailability(t *testing.T) {
	a := testApp(t)
	s := seedSource(t, a)
	if e := os.WriteFile(filepath.Join(s.Root, "broken.mp4"), []byte("not a video"), 0600); e != nil {
		t.Fatal(e)
	}
	mustExec(t, a, `INSERT INTO videos(id,source_id,file_key,title,size,modified,indexed_at,duration,width,height,cover,hidden) VALUES(1,1,'broken.mp4','broken',11,1,1,12,0,0,'existing.jpg',1)`)
	mustExec(t, a, `INSERT INTO videos(id,source_id,file_key,title,size,modified,indexed_at,duration,width,height,cover) VALUES(2,1,'complete.mp4','complete',100,1,1,10,320,180,'complete.jpg')`)
	id, e := a.startMetadataRepair(s.ID)
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(10 * time.Second)
	var state string
	var processed, failed int
	for time.Now().Before(deadline) {
		a.db.QueryRow(`SELECT status,scanned,errors FROM jobs WHERE id=?`, id).Scan(&state, &processed, &failed)
		if state != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if state != "completed" || processed != 1 || failed != 1 {
		t.Fatalf("%s %d %d", state, processed, failed)
	}
	v, e := a.video(1)
	if e != nil || v.Duration != 12 || v.Cover == "" || !v.Hidden || !v.Available {
		t.Fatalf("fields lost: %+v %v", v, e)
	}
	v, e = a.video(2)
	if e != nil || !v.Available {
		t.Fatal("repair pruned untouched record")
	}
}
