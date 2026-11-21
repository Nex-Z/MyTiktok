package app

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalMediaUpload(t *testing.T) {
	a := testApp(t)
	handler := a.Handler()
	post := func(path, data string, status int) {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/local/"+path, strings.NewReader(data)))
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	post("directories?name=album", "", 200)
	post("files?key=album&name=song.mp3", "audio", 200)
	post("files?key=album&name=song.mp3", "replacement", 409)
	post("files?key=album&name=video.mp4", "video", 200)
	post("files?key=..&name=escape.mp4", "", 400)
	post("directories?name=../escape", "", 400)
	post("files?name=script.html", "", 400)
	data, err := os.ReadFile(filepath.Join(a.cfg.MediaRoot, "album", "song.mp3"))
	if err != nil || string(data) != "audio" {
		t.Fatalf("unexpected saved content: %q, %v", data, err)
	}
}
func TestUploadQueuesScanForBusySource(t *testing.T) {
	a := testApp(t)
	mustExec(t, a, "INSERT INTO sources(id,name,kind,root,interval_hours) VALUES(1,'Local','local',?,0)", a.cfg.MediaRoot)
	a.running[1] = func() {}
	for _, name := range []string{"one.mp3", "two.mp4"} {
		w := request(t, a, "POST", "/api/local/files?name="+name, nil)
		if w.Code != 200 || !a.pendingScan[1] {
			t.Fatalf("scan not queued: %s", w.Body.String())
		}
	}
	a.finishSourceJob(1)
	a.wg.Wait()
	var jobs, files int
	if err := a.db.QueryRow("SELECT count(*) FROM jobs WHERE source_id=1 AND status='completed'").Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT count(*) FROM videos WHERE source_id=1 AND available=1").Scan(&files); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || files != 2 {
		t.Fatalf("jobs=%d files=%d", jobs, files)
	}
}
