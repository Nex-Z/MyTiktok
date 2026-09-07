package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeSync struct {
	files      map[string][]Entry
	uploads    []string
	failList   bool
	failUpload bool
	pages      int
}

func (f *fakeSync) List(ctx context.Context, key, cursor string) (Page, error) {
	if e := ctx.Err(); e != nil {
		return Page{}, e
	}
	if f.failList {
		return Page{}, errCloud
	}
	f.pages++
	items := f.files[key]
	offset := intParam(cursor, 0)
	end := min(offset+1, len(items))
	p := Page{Entries: items[offset:end]}
	if end < len(items) {
		p.Next = itoa(int64(end))
	}
	return p, nil
}
func (f *fakeSync) Mkdir(ctx context.Context, parent, name string) (string, error) {
	id := parent + "/" + name
	f.files[parent] = append(f.files[parent], Entry{Key: id, Name: name, Dir: true})
	return id, nil
}
func (f *fakeSync) Upload(ctx context.Context, parent, name string, file *os.File) error {
	if f.failUpload {
		return errors.New("failure")
	}
	i, _ := file.Stat()
	f.uploads = append(f.uploads, parent+"/"+name)
	f.files[parent] = append(f.files[parent], Entry{Key: name, Name: name, Size: i.Size()})
	return nil
}
func syncFixture(t *testing.T) (*App, string) {
	a := testApp(t)
	a.syncWait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	s := seedSource(t, a)
	mustExec(t, a, `INSERT INTO sync_tasks(id,name,source_id,target,interval_hours,enabled) VALUES(1,'test',1,'0',6,0)`)
	mustExec(t, a, `INSERT INTO sync_runs(id,task_id,status,started_at) VALUES(1,1,'running',1)`)
	return a, s.Root
}
func TestSyncSkipsSameNameAcrossPagesAndPreservesTree(t *testing.T) {
	a, root := syncFixture(t)
	os.Mkdir(filepath.Join(root, "nested"), 0700)
	os.WriteFile(filepath.Join(root, "same.mp4"), []byte("changed version"), 0600)
	os.WriteFile(filepath.Join(root, "nested", "new.mp4"), []byte("new"), 0600)
	p := &fakeSync{files: map[string][]Entry{"0": {{Name: "unrelated"}, {Name: "same.mp4", Size: 1}}}}
	if e := a.copyTree(context.Background(), root, "0", 1, p); e != nil {
		t.Fatal(e)
	}
	if len(p.uploads) != 1 || p.uploads[0] != "0/nested/new.mp4" {
		t.Fatal(p.uploads)
	}
	if p.pages < 3 {
		t.Fatal("pagination not traversed")
	}
	if e := a.copyTree(context.Background(), root, "0", 1, p); e != nil {
		t.Fatal(e)
	}
	if len(p.uploads) != 1 {
		t.Fatal("duplicate upload")
	}
	b, _ := os.ReadFile(filepath.Join(root, "same.mp4"))
	if string(b) != "changed version" {
		t.Fatal("local modified")
	}
	var skipped int
	a.db.QueryRow(`SELECT skipped FROM sync_runs WHERE id=1`).Scan(&skipped)
	if skipped != 3 {
		t.Fatal(skipped)
	}
}
func TestSyncFailureAndCancellationDoNotUpload(t *testing.T) {
	a, root := syncFixture(t)
	os.WriteFile(filepath.Join(root, "a.mp4"), []byte("x"), 0600)
	p := &fakeSync{files: map[string][]Entry{}, failList: true}
	if e := a.copyTree(context.Background(), root, "0", 1, p); e == nil {
		t.Fatal("expected listing failure")
	}
	if len(p.uploads) > 0 {
		t.Fatal("uploaded after failed listing")
	}
	p.failList = false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := a.copyTree(ctx, root, "0", 1, p); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	p.failUpload = true
	if e := a.copyTree(context.Background(), root, "0", 1, p); e == nil {
		t.Fatal("expected exhausted retries to stop run")
	}
	var failed int
	a.db.QueryRow(`SELECT failed FROM sync_runs WHERE id=1`).Scan(&failed)
	if failed != 1 {
		t.Fatal("missing failure record")
	}
}
func TestLikesAndSyncValidation(t *testing.T) {
	a, _ := syncFixture(t)
	mustExec(t, a, `INSERT INTO videos(source_id,file_key,title,size,modified,indexed_at) VALUES(1,'a','a',1,1,1)`)
	w := request(t, a, "PUT", "/api/videos/1/like", map[string]bool{"value": true})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request(t, a, "GET", "/api/feed?mode=likes", nil)
	if !strings.Contains(w.Body.String(), `"liked":true`) {
		t.Fatal(w.Body.String())
	}
	w = request(t, a, "POST", "/api/sync/tasks", map[string]any{"name": "bad", "sourceId": 1, "target": "https://example.com", "intervalHours": 6})
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestSyncRestartPreservesRecords(t *testing.T) {
	cfg := Config{DataDir: t.TempDir(), MediaRoot: t.TempDir(), WebDir: t.TempDir()}
	a, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	seedSource(t, a)
	mustExec(t, a, `INSERT INTO sync_tasks VALUES(1,'copy',1,'0',6,0,1)`)
	mustExec(t, a, `INSERT INTO sync_runs(id,task_id,status,started_at) VALUES(1,1,'running',1)`)
	if e = a.syncRecord(1, "done.mp4", "uploaded", "上传成功", 3); e != nil {
		t.Fatal(e)
	}
	a.Close()
	a, e = New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	var status string
	var count int
	a.db.QueryRow(`SELECT status,uploaded FROM sync_runs WHERE id=1`).Scan(&status, &count)
	if status != "interrupted" || count != 1 {
		t.Fatalf("%s %d", status, count)
	}
}
