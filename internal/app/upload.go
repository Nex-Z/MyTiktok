package app

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func validUploadName(name string) bool {
	return filepath.IsLocal(name) && !strings.ContainsAny(name, "/\\:") && name != "." && strings.TrimSpace(name) != ""
}

func (a *App) writeLocalMedia(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	key, name := q.Get("key"), q.Get("name")
	if key == "" {
		key = "."
	}
	batch := r.URL.Path == "/api/local/batch"
	if !fs.ValidPath(key) || !filepath.IsLocal(key) || strings.ContainsAny(key, "\\:") ||
		(!batch && !validUploadName(name)) {
		writeError(w, 400, "invalid_path", "目录或文件名无效")
		return
	}
	upload := r.URL.Path == "/api/local/files"
	if upload && !isVideo(name) && mediaType(name) != "audio" {
		writeError(w, 400, "invalid_type", "请选择支持的音频或视频文件")
		return
	}
	root, err := os.OpenRoot(a.cfg.MediaRoot)
	if err != nil {
		fail(w, err)
		return
	}
	defer root.Close()
	dir, err := root.OpenRoot(filepath.FromSlash(key))
	if err != nil {
		writeError(w, 400, "invalid_directory", "目标目录不存在或不可访问")
		return
	}
	defer dir.Close()
	if batch {
		a.uploadBatch(w, r, dir, key)
		return
	}
	if upload {
		err = saveUpload(dir, name, r.Body)
	} else {
		err = dir.Mkdir(name, 0755)
	}
	if errors.Is(err, os.ErrExist) {
		writeError(w, 409, "exists", "目标目录已有同名项")
		return
	}
	if err != nil {
		writeError(w, 500, "write_failed", "写入失败，请检查挂载是否可写、剩余空间及目录权限")
		return
	}
	if upload {
		message := a.scanAfterUpload(key)
		jsonOK(w, map[string]any{"ok": true, "scanMessage": message})
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

func (a *App) uploadBatch(w http.ResponseWriter, r *http.Request, dir *os.Root, key string) {
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, 400, "invalid_batch", "请选择要上传的文件")
		return
	}
	type item struct {
		Name  string `json:"name"`
		Error string `json:"error,omitempty"`
	}
	results := []item{}
	saved := 0
	message := ""
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			message = "传输中断，请检查未完成的文件。"
			break
		}
		name := part.FileName()
		row := item{Name: name}
		switch {
		case part.FormName() != "files" || !validUploadName(name):
			row.Error = "文件名无效"
		case !isVideo(name) && mediaType(name) != "audio":
			row.Error = "不支持的文件类型"
		default:
			err = saveUpload(dir, name, part)
			switch {
			case err == nil:
				saved++
			case errors.Is(err, os.ErrExist):
				row.Error = "已有同名文件"
			default:
				row.Error = "写入失败，请检查连接、磁盘空间和目录权限"
			}
		}
		results = append(results, row)
	}
	if saved > 0 {
		message += a.scanAfterUpload(key)
	}
	jsonOK(w, map[string]any{"items": results, "saved": saved, "message": message})
}

func (a *App) scanAfterUpload(key string) string {
	dir, err := safePath(a.cfg.MediaRoot, key)
	if err != nil {
		return "无法定位上传目录，请手动扫描。"
	}
	sources, err := a.sources()
	if err != nil {
		return "无法获取媒体源，请手动扫描。"
	}
	matched, failed := false, false
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range sources {
		if s.Kind != "local" {
			continue
		}
		rel, err := filepath.Rel(s.Root, dir)
		if err != nil || !filepath.IsLocal(rel) {
			continue
		}
		matched = true
		if _, busy := a.running[s.ID]; busy {
			a.pendingScan[s.ID] = true
		} else if _, err := a.startScanLocked(s.ID); err != nil {
			failed = true
		}
	}
	if failed {
		return "部分扫描未能启动，请查看扫描任务。"
	}
	if !matched {
		return "该目录尚未添加到本地媒体源，添加后会自动扫描。"
	}
	return "已安排扫描，可在扫描任务中查看进度。"
}

func saveUpload(dir *os.Root, name string, body io.Reader) error {
	tmp := ".upload-" + token() + ".part"
	f, err := dir.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer dir.Remove(tmp)
	_, err = io.Copy(f, body)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Publish only complete files and preserve concurrent same-name uploads.
	return dir.Link(tmp, name)
}
