// Package server 提供播放器的 HTTP 服务：内嵌前端页面、JSON API 与音频文件流。
// 仅监听 127.0.0.1，音频接口只允许访问音乐库内或歌单中已登记的文件。
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"partyplayer/internal/app"
	"partyplayer/internal/qr"
)

type Server struct {
	app       *app.App
	webFS     fs.FS
	lanURLs   []string
	version   string
	buildTime string
}

func New(a *app.App, webFS fs.FS) *Server {
	return &Server{app: a, webFS: webFS}
}

// SetLanURLs 设置局域网接入地址（用于页面二维码展示）。
func (s *Server) SetLanURLs(urls []string) { s.lanURLs = urls }

// SetVersion 设置构建版本信息（随状态接口下发）。
func (s *Server) SetVersion(version, buildTime string) { s.version, s.buildTime = version, buildTime }

// Register 注册全部路由。
func (s *Server) Register(mux *http.ServeMux) {
	mux.Handle("GET /", http.FileServerFS(s.webFS))
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/library/root", s.handleSetRoot)
	mux.HandleFunc("POST /api/library/rescan", s.handleRescan)
	mux.HandleFunc("POST /api/library/pick-dir", s.handlePickDir)
	mux.HandleFunc("GET /api/library/tree", s.handleTree)
	mux.HandleFunc("POST /api/playlists/mount", s.handlePlaylistMount)
	mux.HandleFunc("POST /api/playlist/generate", s.handleGenerate)
	mux.HandleFunc("POST /api/playlist/set", s.handleSetPlaylist)
	mux.HandleFunc("POST /api/playlist/add", s.handleAdd)
	mux.HandleFunc("POST /api/playlist/move", s.handleMove)
	mux.HandleFunc("POST /api/playlist/remove", s.handleRemove)
	mux.HandleFunc("POST /api/playlist/clear", s.handleClear)
	mux.HandleFunc("POST /api/playlist/shuffle", s.handleShuffle)
	mux.HandleFunc("POST /api/settings", s.handleSettings)
	mux.HandleFunc("POST /api/playlists/save", s.handleSavePlaylist)
	mux.HandleFunc("POST /api/playlists/load", s.handleLoadPlaylist)
	mux.HandleFunc("POST /api/playlists/delete", s.handleDeletePlaylist)
	mux.HandleFunc("POST /api/playlists/import", s.handleImportPlaylist)
	mux.HandleFunc("GET /api/playlists/export", s.handleExportPlaylist)
	mux.HandleFunc("POST /api/durations", s.handleDurations)
	mux.HandleFunc("GET /audio", s.handleAudio)
	mux.HandleFunc("GET /qr", s.handleQR)
	mux.HandleFunc("POST /api/shutdown", s.handleShutdown)
}

// ---- 通用工具 ----

type apiResp struct {
	OK    bool           `json:"ok"`
	Error string         `json:"error,omitempty"`
	State *app.StateView `json:"state,omitempty"`
}

func (s *Server) writeState(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp := apiResp{OK: err == nil}
	if err != nil {
		resp.Error = err.Error()
	} else {
		st := s.app.State()
		st.LanURLs = s.lanURLs
		st.Version = s.version
		st.BuildTime = s.buildTime
		resp.State = &st
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 10<<20))
	return dec.Decode(v)
}

// ---- API 处理器 ----

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	s.writeState(w, nil)
}

func (s *Server) handleSetRoot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root string `json:"root"`
	}
	if err := s.decodeJSON(r, &req); err != nil {
		s.writeState(w, err)
		return
	}
	s.writeState(w, s.app.SetMusicRoot(strings.TrimSpace(req.Root)))
}

func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	s.writeState(w, s.app.Rescan())
}

// handlePickDir 弹出系统原生目录选择框（桌面端）。阻塞至用户选择或取消。
func (s *Server) handlePickDir(w http.ResponseWriter, r *http.Request) {
	path, err := pickDirectory("选择舞会曲库目录（一级子目录 = 舞种）")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp := struct {
		OK    bool   `json:"ok"`
		Path  string `json:"path,omitempty"`
		Error string `json:"error,omitempty"`
	}{OK: err == nil, Path: path}
	if err != nil {
		resp.Error = err.Error()
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req app.GenerateRequest
	if err := s.decodeJSON(r, &req); err != nil {
		s.writeState(w, err)
		return
	}
	_, err := s.app.Generate(req)
	s.writeState(w, err)
}

func (s *Server) handleSetPlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tracks []app.Track `json:"tracks"`
	}
	if err := s.decodeJSON(r, &req); err != nil {
		s.writeState(w, err)
		return
	}
	s.app.SetPlaylist(req.Tracks)
	s.writeState(w, nil)
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := s.decodeJSON(r, &req); err != nil {
		s.writeState(w, err)
		return
	}
	_, err := s.app.AddTrack(req.Path)
	s.writeState(w, err)
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Index int    `json:"index"`
		Dir   string `json:"dir"`
	}
	if err := s.decodeJSON(r, &req); err != nil {
		s.writeState(w, err)
		return
	}
	s.writeState(w, s.app.MovePlaylist(req.Index, req.Dir))
}

func (s *Server) handleRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Index int `json:"index"`
	}
	if err := s.decodeJSON(r, &req); err != nil {
		s.writeState(w, err)
		return
	}
	s.writeState(w, s.app.RemovePlaylist(req.Index))
}

func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	s.app.ClearPlaylist()
	s.writeState(w, nil)
}

func (s *Server) handleShuffle(w http.ResponseWriter, r *http.Request) {
	s.app.ShufflePlaylist()
	s.writeState(w, nil)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	var req app.Settings
	if err := s.decodeJSON(r, &req); err != nil {
		s.writeState(w, err)
		return
	}
	s.app.UpdateSettings(req)
	s.writeState(w, nil)
}

func (s *Server) handleSavePlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Mount string `json:"mount"` // 为空时默认当前音乐库根目录
	}
	if err := s.decodeJSON(r, &req); err != nil {
		s.writeState(w, err)
		return
	}
	s.writeState(w, s.app.SavePlaylist(strings.TrimSpace(req.Name), strings.TrimSpace(req.Mount)))
}

func (s *Server) handleLoadPlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := s.decodeJSON(r, &req); err != nil {
		s.writeState(w, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	_, mount, err := s.app.LoadPlaylist(name)
	if err != nil {
		s.writeState(w, err)
		return
	}
	// 歌单的挂载目录即其曲库：与当前根不同则自动切换并重扫描
	if mount != "" && mount != s.app.MusicRoot() {
		if err := s.app.SetMusicRoot(mount); err != nil {
			log.Printf("切换到歌单挂载目录失败: %v", err)
		}
	}
	s.writeState(w, nil)
}

// handlePlaylistMount 更新已保存歌单的挂载目录。
func (s *Server) handlePlaylistMount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Dir  string `json:"dir"`
	}
	if err := s.decodeJSON(r, &req); err != nil {
		s.writeState(w, err)
		return
	}
	s.writeState(w, s.app.SetPlaylistMount(strings.TrimSpace(req.Name), strings.TrimSpace(req.Dir)))
}

// handleTree 浏览目录一层内容（添加乐曲的文件树）。
func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	v, err := s.app.BrowseDir(dir)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp := struct {
		OK    bool         `json:"ok"`
		Error string       `json:"error,omitempty"`
		Dir   *app.DirView `json:"dir,omitempty"`
	}{OK: err == nil}
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.Dir = &v
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleDeletePlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := s.decodeJSON(r, &req); err != nil {
		s.writeState(w, err)
		return
	}
	s.writeState(w, s.app.DeletePlaylist(strings.TrimSpace(req.Name)))
}

func (s *Server) handleImportPlaylist(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		s.writeState(w, err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.writeState(w, errors.New("请选择要导入的歌单文件"))
		return
	}
	defer file.Close()
	_, err = s.app.ImportPlaylist(header.Filename, file)
	s.writeState(w, err)
}

func (s *Server) handleExportPlaylist(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	data, filename, err := s.app.ExportPlaylist(q.Get("name"), q.Get("format"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", contentTypeFor(filename))
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename*=UTF-8''%s", escapeURLPath(filename)))
	_, _ = w.Write(data)
}

func (s *Server) handleDurations(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []struct {
			Path    string  `json:"path"`
			Seconds float64 `json:"seconds"`
		} `json:"items"`
	}
	if err := s.decodeJSON(r, &req); err != nil {
		s.writeState(w, err)
		return
	}
	for _, it := range req.Items {
		s.app.PutDuration(it.Path, it.Seconds)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"ok":true}`))
	log.Println("收到退出请求，正在关闭…")
	go os.Exit(0)
}

// handleQR 生成二维码 PNG（/qr?text=...&scale=8）。
func (s *Server) handleQR(w http.ResponseWriter, r *http.Request) {
	text := r.URL.Query().Get("text")
	if text == "" {
		if len(s.lanURLs) > 0 {
			text = s.lanURLs[0]
		} else {
			http.Error(w, "缺少 text 参数", http.StatusBadRequest)
			return
		}
	}
	if len(text) > 106 {
		http.Error(w, "文本过长", http.StatusBadRequest)
		return
	}
	scale := 8
	if v := r.URL.Query().Get("scale"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 2 && n <= 32 {
			scale = n
		}
	}
	img, err := qr.PNG(text, scale)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_ = png.Encode(w, img)
}

// ---- 音频流 ----

// handleAudio 提供音频文件（支持 Range，浏览器可拖动进度）。
// 仅允许：音乐库根目录下的文件，或当前/已保存歌单中出现过的确切路径。
func (s *Server) handleAudio(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("p")
	if raw == "" {
		http.Error(w, "缺少参数 p", http.StatusBadRequest)
		return
	}
	path, err := filepath.Abs(raw)
	if err != nil {
		http.Error(w, "路径不合法", http.StatusBadRequest)
		return
	}
	if !s.pathAllowed(path) {
		http.Error(w, "路径不在允许范围内", http.StatusForbidden)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "文件无法打开", http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.Error(w, "文件不存在", http.StatusNotFound)
		return
	}
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, filepath.Base(path), st.ModTime(), f)
}

func (s *Server) pathAllowed(path string) bool {
	return s.app.PathAllowed(path)
}

func contentTypeFor(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".m3u", ".m3u8":
		return "audio/x-mpegurl; charset=utf-8"
	case ".pls":
		return "audio/x-scpls; charset=utf-8"
	case ".csv":
		return "text/csv; charset=utf-8"
	default:
		return "application/json; charset=utf-8"
	}
}

func escapeURLPath(s string) string {
	var sb strings.Builder
	for _, b := range []byte(s) {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') ||
			b == '-' || b == '.' || b == '_' || b == '~' {
			sb.WriteByte(b)
		} else {
			fmt.Fprintf(&sb, "%%%02X", b)
		}
	}
	return sb.String()
}
