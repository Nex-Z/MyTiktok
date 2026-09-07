package app

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"path/filepath"
	"testing"
)

func feedIDs(t *testing.T, a *App, path string) []int64 {
	t.Helper()
	w := request(t, a, "GET", path, nil)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var f struct {
		Items []Video `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for _, v := range f.Items {
		ids = append(ids, v.ID)
	}
	return ids
}
func TestSearchAndFiltersAcrossLibraryModes(t *testing.T) {
	a := testApp(t)
	seedSource(t, a)
	mustExec(t, a, `INSERT INTO sources(id,name,kind,root) VALUES(2,'Cloud','p115','0')`)
	for _, v := range []struct {
		id, source              int
		title, folder           string
		duration, width, height int
	}{{1, 1, "Ocean 100%.mp4", "Trip", 30, 1080, 1920}, {2, 1, "OCEAN waves.mp4", "Trip/Day2", 120, 1920, 1080}, {3, 2, "Ocean long.mp4", "Trip", 600, 1080, 1080}, {4, 1, "Other.mp4", "Trip2", 0, 0, 0}} {
		mustExec(t, a, `INSERT INTO videos(id,source_id,file_key,title,size,modified,indexed_at,folder,duration,width,height) VALUES(?,?,?, ?,100,1000000000000000000,1,?,?,?,?)`, v.id, v.source, v.title, v.title, v.folder, v.duration, v.width, v.height)
		mustExec(t, a, `INSERT INTO favorites VALUES(?,1)`, v.id)
		mustExec(t, a, `INSERT INTO history VALUES(?,10,1)`, v.id)
	}
	for _, mode := range []string{"feed", "favorites", "history"} {
		ids := feedIDs(t, a, "/api/feed?mode="+mode+"&q=ocean&sourceId=1&duration=short&orientation=portrait")
		if len(ids) != 1 || ids[0] != 1 {
			t.Fatalf("mode %s: %v", mode, ids)
		}
		ids = feedIDs(t, a, "/api/feed?mode="+mode+"&q="+url.QueryEscape("100%"))
		if len(ids) != 1 || ids[0] != 1 {
			t.Fatalf("literal search %v", ids)
		}
	}
	ids := feedIDs(t, a, "/api/feed?sourceId=1&folder=Trip&recursive=1&sort=title")
	if len(ids) != 2 {
		t.Fatal("folder boundary", ids)
	}
	ids = feedIDs(t, a, "/api/feed?sourceId=1&folder=Trip")
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatal("exact folder", ids)
	}
	ids = feedIDs(t, a, "/api/feed?duration=unknown&orientation=unknown")
	if len(ids) != 1 || ids[0] != 4 {
		t.Fatal(ids)
	}
	v, err := a.video(1)
	if err != nil || v.Folder != "Trip" || v.SourceName != "Local" || v.ModifiedAt != 1000000000 {
		t.Fatalf("details %+v %v", v, err)
	}
	if w := request(t, a, "GET", "/api/feed?duration=invalid", nil); w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestIndexedFoldersAndHiddenExclusion(t *testing.T) {
	a := testApp(t)
	seedSource(t, a)
	for i, folder := range []string{"Trip", "Trip/Day2", "Trip2", "Trip/Day2"} {
		mustExec(t, a, `INSERT INTO videos(source_id,file_key,title,size,modified,indexed_at,folder,hidden) VALUES(1,?,'v',1,1,1,?,?)`, i, folder, i == 3)
	}
	w := request(t, a, "GET", "/api/library/folders?sourceId=1&parent=Trip", nil)
	var out []struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Path != "Trip/Day2" || out[0].Count != 1 {
		t.Fatal(w.Body.String())
	}
}
func TestFolderMigrationPreservesExistingLibrary(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schema + `INSERT INTO sources(id,name,kind,root) VALUES(1,'Legacy','local','/media');INSERT INTO videos(id,source_id,file_key,title,size,modified,indexed_at,hidden) VALUES(1,1,'Trip/Day1/a.mp4','a.mp4',1,1,1,1);INSERT INTO favorites VALUES(1,1);INSERT INTO history VALUES(1,12,1);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var folder string
	var hidden, version, fav int
	var pos float64
	if err = db.QueryRow(`SELECT folder,hidden,(SELECT version FROM schema_version),(SELECT count(*) FROM favorites),(SELECT position FROM history) FROM videos WHERE id=1`).Scan(&folder, &hidden, &version, &fav, &pos); err != nil {
		t.Fatal(err)
	}
	if folder != "Trip/Day1" || hidden != 1 || version != 2 || fav != 1 || pos != 12 {
		t.Fatal(folder, hidden, version, fav, pos)
	}
}
