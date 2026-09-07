package app

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMixedMediaScanAndFilter(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("ffmpeg unavailable")
	}
	a := testApp(t)
	s := seedSource(t, a)
	for _, args := range [][]string{{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "2", filepath.Join(s.Root, "tone.wav")}, {"-v", "error", "-f", "lavfi", "-i", "color=c=red:s=320x180", "-frames:v", "1", filepath.Join(s.Root, "still.jpg")}} {
		if b, e := exec.Command("ffmpeg", args...).CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	if e := a.scan(context.Background(), s, 1); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"audio", "image"} {
		w := request(t, a, "GET", "/api/feed?mediaType="+kind, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":1`) {
			t.Fatal(w.Body.String())
		}
	}
	var duration float64
	var cover string
	a.db.QueryRow(`SELECT duration FROM videos WHERE title='tone.wav'`).Scan(&duration)
	if duration < 1.9 {
		t.Fatal("audio duration missing")
	}
	a.db.QueryRow(`SELECT cover FROM videos WHERE title='still.jpg'`).Scan(&cover)
	if cover == "" {
		t.Fatal("image thumbnail missing")
	}
}
