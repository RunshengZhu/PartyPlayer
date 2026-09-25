package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Track 是一首歌。歌单、音乐库均以绝对路径为准。
type Track struct {
	Path     string `json:"path"`              // 绝对路径
	Title    string `json:"title"`             // 文件名去扩展名
	Category string `json:"category"`          // 舞种（一级子目录名，根目录文件为 ""）
	Missing  bool   `json:"missing,omitempty"` // 文件已不存在
}

// audioExts 支持的音频扩展名（浏览器原生可播放的格式）。
var audioExts = map[string]bool{
	".mp3": true, ".wav": true, ".ogg": true, ".oga": true,
	".flac": true, ".m4a": true, ".aac": true, ".opus": true, ".webm": true,
}

// Library 保存一次音乐库扫描的结果。
type Library struct {
	root  string
	cats  []CategoryView // 已按名称排序
	byCat map[string]int
}

// ScanLibrary 扫描 root：一级子目录按舞种别名归一化为标准舞种（摩登/拉丁/交谊舞
// 分组与 A 组顺序，见 genre.go），同舞种的多个目录自动合并，目录内递归收集音频。
// 无法识别舞种的目录归入「其它」组；根目录直属音频归入分类 ""（前端显示为"根目录"）。
func ScanLibrary(root string) *Library {
	lib := &Library{root: root, byCat: map[string]int{}}
	entries, err := os.ReadDir(root)
	if err != nil {
		return lib
	}
	catTracks := map[string][]Track{}
	catGroup := map[string]string{}
	// 根目录直属音频
	for _, e := range entries {
		if e.IsDir() || !isAudio(e.Name()) {
			continue
		}
		p := filepath.Join(root, e.Name())
		if track, ok := newTrack(p, ""); ok {
			catTracks[""] = append(catTracks[""], track)
			catGroup[""] = "其它"
		}
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		group, name := LookupGenre(e.Name())
		dir := filepath.Join(root, e.Name())
		for _, t := range collectTracks(dir, name) {
			catTracks[name] = append(catTracks[name], t)
			catGroup[name] = group
		}
	}
	for name, tracks := range catTracks {
		if len(tracks) == 0 {
			continue
		}
		sortTracks(tracks)
		lib.cats = append(lib.cats, CategoryView{Name: name, Group: catGroup[name], Tracks: tracks})
	}
	sort.Slice(lib.cats, func(i, j int) bool {
		a, b := lib.cats[i], lib.cats[j]
		if (a.Name == "") != (b.Name == "") {
			return a.Name == "" // 根目录排最前
		}
		return genreLess(a.Name, b.Name)
	})
	for i, c := range lib.cats {
		lib.byCat[c.Name] = i
	}
	return lib
}

func sortTracks(tracks []Track) {
	sort.Slice(tracks, func(i, j int) bool {
		return strings.ToLower(tracks[i].Title) < strings.ToLower(tracks[j].Title)
	})
}

// collectTracks 递归收集 dir 下的全部音频（含舞种内嵌套子目录，如 special/圣诞特色曲目）。
func collectTracks(dir, cat string) []Track {
	var tracks []Track
	sub, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, f := range sub {
		p := filepath.Join(dir, f.Name())
		if f.IsDir() {
			tracks = append(tracks, collectTracks(p, cat)...)
			continue
		}
		if track, ok := newTrack(p, cat); ok {
			tracks = append(tracks, track)
		}
	}
	return tracks
}

func newTrack(path, cat string) (Track, bool) {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(base))
	if !audioExts[ext] {
		return Track{}, false
	}
	return Track{
		Path:     path,
		Title:    strings.TrimSuffix(base, filepath.Ext(base)),
		Category: cat,
	}, true
}

func isAudio(name string) bool {
	return audioExts[strings.ToLower(filepath.Ext(name))]
}

// Categories 返回按舞种排序的分类视图（副本）。
func (l *Library) Categories() []CategoryView {
	out := make([]CategoryView, len(l.cats))
	for i, c := range l.cats {
		tracks := make([]Track, len(c.Tracks))
		copy(tracks, c.Tracks)
		out[i] = CategoryView{Name: c.Name, Group: c.Group, Tracks: tracks}
	}
	return out
}

// FindTrack 在库中按路径查找（用于添加歌曲、导入校验）。
func (a *App) FindTrack(path string) (Track, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lib == nil {
		return Track{}, false
	}
	for _, c := range a.lib.cats {
		for _, t := range c.Tracks {
			if t.Path == path {
				return t, true
			}
		}
	}
	return Track{}, false
}

// FindByTitle 在库中按文件名模糊查找（CSV 导入兜底）。
func (a *App) FindByTitle(filename string) (Track, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lib == nil {
		return Track{}, false
	}
	filename = strings.ToLower(filename)
	for _, c := range a.lib.cats {
		for _, t := range c.Tracks {
			if strings.ToLower(filepath.Base(t.Path)) == filename {
				return t, true
			}
		}
	}
	return Track{}, false
}

// ---- 目录浏览（添加乐曲的文件树） ----

// DirView 某个目录一层的视图：子目录 + 音频文件。
type DirView struct {
	Path  string   `json:"path"`
	Dirs  []string `json:"dirs"`
	Files []Track  `json:"files"`
}

// BrowseDir 浏览 dir 的一层内容。仅允许音乐库根目录及各歌单挂载目录（含其子目录）。
func (a *App) BrowseDir(dir string) (DirView, error) {
	a.mu.Lock()
	root := a.cfg.MusicRoot
	mounts := a.mountedDirsLocked()
	a.mu.Unlock()

	if !dirAllowed(dir, append(mounts, root)...) {
		return DirView{}, errors.New("目录不在允许范围内（仅可浏览音乐库与歌单挂载目录）")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return DirView{}, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return DirView{}, err
	}
	v := DirView{Path: abs}
	for _, e := range entries {
		if e.IsDir() {
			if !strings.HasPrefix(e.Name(), ".") {
				v.Dirs = append(v.Dirs, e.Name())
			}
			continue
		}
		if t, ok := newTrack(filepath.Join(abs, e.Name()), ""); ok {
			v.Files = append(v.Files, t)
		}
	}
	sort.Strings(v.Dirs)
	sort.Slice(v.Files, func(i, j int) bool { return v.Files[i].Title < v.Files[j].Title })
	return v, nil
}

func dirAllowed(dir string, roots ...string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for _, r := range roots {
		if r == "" {
			continue
		}
		rAbs, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		if abs == rAbs || strings.HasPrefix(abs, rAbs+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// PathAllowed 音频接口白名单：音乐库子树、任一歌单挂载目录子树、歌单已登记的确切路径。
func (a *App) PathAllowed(path string) bool {
	a.mu.Lock()
	root := a.cfg.MusicRoot
	mounts := a.mountedDirsLocked()
	var listed []string
	for _, t := range a.current {
		listed = append(listed, t.Path)
	}
	a.mu.Unlock()

	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if dirAllowed(abs, append(mounts, root)...) {
		return true
	}
	for _, p := range listed {
		if p == abs {
			return true
		}
	}
	// 已保存歌单中登记的路径
	entries, err := os.ReadDir(a.playlistsDir())
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(a.playlistsDir(), e.Name()))
		if err != nil {
			continue
		}
		var doc savedPlaylist
		if json.Unmarshal(b, &doc) != nil {
			continue
		}
		for _, t := range doc.Tracks {
			if t.Path == abs {
				return true
			}
		}
	}
	return false
}
