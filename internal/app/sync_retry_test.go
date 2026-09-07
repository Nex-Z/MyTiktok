package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type unreliableSync struct {
	*fakeSync
	calls        int
	failures     int
	lostResponse bool
}

func (f *unreliableSync) Upload(ctx context.Context, parent, name string, file *os.File) error {
	f.calls++
	if f.calls <= f.failures {
		return errCloud
	}
	e := f.fakeSync.Upload(ctx, parent, name, file)
	if f.lostResponse {
		return errCloud
	}
	return e
}
func TestSyncLostResponseIsReconciledWithoutDuplicate(t *testing.T) {
	a, root := syncFixture(t)
	os.WriteFile(filepath.Join(root, "a.mp4"), []byte("data"), 0600)
	p := &unreliableSync{fakeSync: &fakeSync{files: map[string][]Entry{}}, lostResponse: true}
	if e := a.copyTree(context.Background(), root, "0", 1, p); e != nil {
		t.Fatal(e)
	}
	if p.calls != 1 {
		t.Fatalf("duplicate upload: %d", p.calls)
	}
	var uploaded, failed, records int
	a.db.QueryRow(`SELECT uploaded,failed FROM sync_runs WHERE id=1`).Scan(&uploaded, &failed)
	a.db.QueryRow(`SELECT count(*) FROM sync_records WHERE run_id=1`).Scan(&records)
	if uploaded != 1 || failed != 0 || records != 1 {
		t.Fatalf("counts %d %d %d", uploaded, failed, records)
	}
}
func TestSync500FilesRetryAndResume(t *testing.T) {
	a, root := syncFixture(t)
	for i := 0; i < 500; i++ {
		if e := os.WriteFile(filepath.Join(root, fmt.Sprintf("%03d.mp4", i)), []byte("data"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	p := &unreliableSync{fakeSync: &fakeSync{files: map[string][]Entry{}}, failures: 3}
	if e := a.copyTree(context.Background(), root, "0", 1, p); e == nil {
		t.Fatal("expected stop")
	}
	if p.calls != 3 {
		t.Fatalf("continued after exhausted retries: %d", p.calls)
	}
	mustExec(t, a, `INSERT INTO sync_runs(id,task_id,status,started_at) VALUES(2,1,'running',2)`)
	if e := a.copyTree(context.Background(), root, "0", 2, p); e != nil {
		t.Fatal(e)
	}
	if len(p.uploads) != 500 {
		t.Fatal(len(p.uploads))
	}
	var count int
	a.db.QueryRow(`SELECT uploaded FROM sync_runs WHERE id=2`).Scan(&count)
	if count != 500 {
		t.Fatal(count)
	}
	mustExec(t, a, `INSERT INTO sync_runs(id,task_id,status,started_at) VALUES(3,1,'running',3)`)
	if e := a.copyTree(context.Background(), root, "0", 3, p); e != nil {
		t.Fatal(e)
	}
	if len(p.uploads) != 500 {
		t.Fatal("reuploaded completed files")
	}
	a.db.QueryRow(`SELECT skipped FROM sync_runs WHERE id=3`).Scan(&count)
	if count != 500 {
		t.Fatal(count)
	}
}
func TestSyncCooldownCanBeCanceled(t *testing.T) {
	a, root := syncFixture(t)
	os.WriteFile(filepath.Join(root, "a.mp4"), []byte("x"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	a.syncWait = func(ctx context.Context, d time.Duration) error {
		if d >= 30*time.Second {
			cancel()
		}
		return ctx.Err()
	}
	p := &unreliableSync{fakeSync: &fakeSync{files: map[string][]Entry{}}, failures: 3}
	if e := a.copyTree(ctx, root, "0", 1, p); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	var status string
	a.db.QueryRow(`SELECT status FROM sync_records WHERE run_id=1`).Scan(&status)
	if status != "unconfirmed" {
		t.Fatal(status)
	}
	if p.calls != 1 {
		t.Fatal(p.calls)
	}
}
func TestSyncInFlightSurvivesRestart(t *testing.T) {
	cfg := Config{DataDir: t.TempDir(), MediaRoot: t.TempDir(), WebDir: t.TempDir()}
	a, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	seedSource(t, a)
	mustExec(t, a, `INSERT INTO sync_tasks VALUES(1,'copy',1,'0',6,0,1)`)
	mustExec(t, a, `INSERT INTO sync_runs(id,task_id,status,started_at) VALUES(1,1,'running',1)`)
	if e = a.syncRecord(1, "pending.mp4", "uploading", "in flight", 4); e != nil {
		t.Fatal(e)
	}
	a.Close()
	a, e = New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	var status string
	var failed int
	a.db.QueryRow(`SELECT status FROM sync_records WHERE run_id=1`).Scan(&status)
	a.db.QueryRow(`SELECT failed FROM sync_runs WHERE id=1`).Scan(&failed)
	if status != "unconfirmed" || failed != 1 {
		t.Fatalf("%s %d", status, failed)
	}
}
func TestSyncTransientFailureRetriesWithBackoff(t *testing.T) {
	a, root := syncFixture(t)
	os.WriteFile(filepath.Join(root, "retry.mp4"), []byte("data"), 0600)
	var delays []time.Duration
	a.syncWait = func(ctx context.Context, d time.Duration) error { delays = append(delays, d); return ctx.Err() }
	p := &unreliableSync{fakeSync: &fakeSync{files: map[string][]Entry{}}, failures: 2}
	if e := a.copyTree(context.Background(), root, "0", 1, p); e != nil {
		t.Fatal(e)
	}
	if p.calls != 3 || len(delays) != 3 || delays[0] != 3*time.Second || delays[1] != 30*time.Second || delays[2] != 60*time.Second {
		t.Fatalf("%d %v", p.calls, delays)
	}
}
func TestSyncRateBackoffIsTenMinutes(t *testing.T) {
	a, _ := syncFixture(t)
	var delay time.Duration
	a.syncWait = func(ctx context.Context, d time.Duration) error { delay = d; return nil }
	if e := a.syncBackoff(context.Background(), 1, 1, errRate); e != nil {
		t.Fatal(e)
	}
	if delay != 10*time.Minute {
		t.Fatal(delay)
	}
}
