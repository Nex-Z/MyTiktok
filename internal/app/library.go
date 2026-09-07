package app

import (
	"net/http"
	"sort"
	"strings"
)

// Folder navigation is derived from indexed videos, so browsing never contacts a provider.
func (a *App) libraryFolders(w http.ResponseWriter, r *http.Request) {
	sourceID := intParam(r.URL.Query().Get("sourceId"), 0)
	if sourceID == 0 {
		writeError(w, 400, "invalid_filter", "请选择媒体源")
		return
	}
	parent := r.URL.Query().Get("parent")
	prefix := ""
	if parent != "" {
		prefix = parent + "/"
	}
	rows, err := a.db.Query(`SELECT folder,count(*) FROM videos WHERE source_id=? AND hidden=0 AND available=1 GROUP BY folder`, sourceID)
	if err != nil {
		fail(w, err)
		return
	}
	defer rows.Close()
	type folder struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Count int    `json:"count"`
	}
	counts := map[string]int{}
	for rows.Next() {
		var name string
		var count int
		if err = rows.Scan(&name, &count); err != nil {
			fail(w, err)
			return
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimPrefix(name, prefix)
		if rest == "" {
			continue
		}
		child := strings.SplitN(rest, "/", 2)[0]
		counts[child] += count
	}
	if err = rows.Err(); err != nil {
		fail(w, err)
		return
	}
	out := []folder{}
	for name, count := range counts {
		out = append(out, folder{name, prefix + name, count})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	jsonOK(w, out)
}
