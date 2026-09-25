// Package app 管理播放器的全部状态：设置、音乐库、歌单，以及它们的本地持久化。
// 所有状态保存在数据目录（默认用户配置目录下 PartyPlayer）中的 JSON 文件里。
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// SettingsGroup 一组播放设置：单曲限时 / 渐弱 / 间隔。
type SettingsGroup struct {
	PlayLimitSec int `json:"play_limit_sec"` // 单曲最长播放秒数，0 = 完整播放
	FadeoutSec   int `json:"fadeout_sec"`    // 到时前的渐弱秒数
	GapSec       int `json:"gap_sec"`        // 歌单：歌曲间隔
}

// CompSettings 点播模式设置：歌曲间隔（舞种内）与舞种间隔分开调节。
type CompSettings struct {
	PlayLimitSec int `json:"play_limit_sec"` // 单曲最长播放秒数，0 = 完整播放
	FadeoutSec   int `json:"fadeout_sec"`    // 到时前的渐弱秒数
	SongGapSec   int `json:"song_gap_sec"`   // 舞种内两首歌之间的间隔
	GenreGapSec  int `json:"genre_gap_sec"`  // 舞种之间的间隔
}

// Settings 歌单与点播两组设置相互独立。
type Settings struct {
	Party SettingsGroup `json:"party"`
	Comp  CompSettings  `json:"comp"`
}

// normalize 校验并修正单组设置。
func (g SettingsGroup) normalize() SettingsGroup {
	if g.PlayLimitSec < 0 {
		g.PlayLimitSec = 0
	}
	if g.FadeoutSec < 0 {
		g.FadeoutSec = 0
	}
	if g.GapSec < 0 {
		g.GapSec = 0
	}
	if g.FadeoutSec > 0 && g.PlayLimitSec > 0 && g.FadeoutSec >= g.PlayLimitSec {
		g.FadeoutSec = g.PlayLimitSec - 1
	}
	return g
}

func (c CompSettings) normalize() CompSettings {
	if c.PlayLimitSec < 0 {
		c.PlayLimitSec = 0
	}
	if c.FadeoutSec < 0 {
		c.FadeoutSec = 0
	}
	if c.SongGapSec < 0 {
		c.SongGapSec = 0
	}
	if c.GenreGapSec < 0 {
		c.GenreGapSec = 0
	}
	if c.FadeoutSec > 0 && c.PlayLimitSec > 0 && c.FadeoutSec >= c.PlayLimitSec {
		c.FadeoutSec = c.PlayLimitSec - 1
	}
	return c
}

type config struct {
	MusicRoot string   `json:"music_root"`
	Settings  Settings `json:"settings"`
}

// App 是播放器应用状态。并发安全。
type App struct {
	mu      sync.Mutex
	dataDir string
	cfg     config
	lib     *Library           // 音乐库扫描结果（root 为空时为 nil）
	current []Track            // 当前歌单（含缺失标记）
	dur     map[string]float64 // 时长缓存 path -> 秒
}

// Open 初始化（必要时创建）数据目录并加载已有状态。
func Open(dataDir string) (*App, error) {
	if dataDir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		dataDir = filepath.Join(base, "PartyPlayer")
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "playlists"), 0o755); err != nil {
		return nil, err
	}
	a := &App{
		dataDir: dataDir,
		dur:     map[string]float64{},
	}
	a.loadConfig()
	if a.cfg.MusicRoot != "" {
		a.rescanLocked()
	}
	a.loadCurrent()
	a.loadDurations()
	return a, nil
}

func (a *App) DataDir() string { return a.dataDir }

func (a *App) MusicRoot() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.MusicRoot
}

func (a *App) cfgPath() string       { return filepath.Join(a.dataDir, "config.json") }
func (a *App) currentPath() string   { return filepath.Join(a.dataDir, "current.json") }
func (a *App) durationsPath() string { return filepath.Join(a.dataDir, "durations.json") }
func (a *App) playlistsDir() string  { return filepath.Join(a.dataDir, "playlists") }

func (a *App) loadConfig() {
	b, err := os.ReadFile(a.cfgPath())
	if err == nil {
		_ = json.Unmarshal(b, &a.cfg)
	}
	if a.cfg.Settings.Party == (SettingsGroup{}) {
		// 尝试迁移旧版平铺格式（v0.1.0-20260924 及之前）
		var legacy struct {
			Settings struct {
				PlayLimitSec int `json:"play_limit_sec"`
				FadeoutSec   int `json:"fadeout_sec"`
				GapSec       int `json:"gap_sec"`
			} `json:"settings"`
		}
		if err == nil && json.Unmarshal(b, &legacy) == nil && legacy.Settings != (struct {
			PlayLimitSec int `json:"play_limit_sec"`
			FadeoutSec   int `json:"fadeout_sec"`
			GapSec       int `json:"gap_sec"`
		}{}) {
			a.cfg.Settings.Party = SettingsGroup{
				PlayLimitSec: legacy.Settings.PlayLimitSec,
				FadeoutSec:   legacy.Settings.FadeoutSec,
				GapSec:       legacy.Settings.GapSec,
			}
		}
	}
	// 默认：舞会常用——每首 90 秒、最后 5 秒渐弱、间隔 2 秒；点播默认舞种间隔 10 秒
	if a.cfg.Settings.Party == (SettingsGroup{}) {
		a.cfg.Settings.Party = SettingsGroup{PlayLimitSec: 90, FadeoutSec: 5, GapSec: 2}
	}
	if a.cfg.Settings.Comp == (CompSettings{}) {
		a.cfg.Settings.Comp = CompSettings{PlayLimitSec: 90, FadeoutSec: 5, SongGapSec: 2, GenreGapSec: 10}
	}
	a.cfg.Settings.Party = a.cfg.Settings.Party.normalize()
	a.cfg.Settings.Comp = a.cfg.Settings.Comp.normalize()
}

func (a *App) saveConfigLocked() {
	b, _ := json.MarshalIndent(a.cfg, "", "  ")
	_ = os.WriteFile(a.cfgPath(), b, 0o644)
}

func (a *App) loadCurrent() {
	b, err := os.ReadFile(a.currentPath())
	if err != nil {
		return
	}
	var saved struct {
		Tracks []Track `json:"tracks"`
	}
	if json.Unmarshal(b, &saved) == nil {
		a.current = saved.Tracks
		a.refreshMissingLocked()
	}
}

func (a *App) saveCurrentLocked() {
	b, _ := json.MarshalIndent(struct {
		Tracks []Track `json:"tracks"`
	}{a.current}, "", "  ")
	_ = os.WriteFile(a.currentPath(), b, 0o644)
}

func (a *App) loadDurations() {
	b, err := os.ReadFile(a.durationsPath())
	if err == nil {
		_ = json.Unmarshal(b, &a.dur)
	}
}

// ---- 音乐库 ----

// SetMusicRoot 设置音乐库根目录并立即扫描。
func (a *App) SetMusicRoot(root string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if root == "" {
		return errors.New("路径为空")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return fmt.Errorf("目录不存在或不可读: %s", abs)
	}
	a.cfg.MusicRoot = abs
	a.rescanLocked()
	a.saveConfigLocked()
	return nil
}

// Rescan 重新扫描音乐库。
func (a *App) Rescan() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.MusicRoot == "" {
		return errors.New("尚未设置音乐库")
	}
	a.rescanLocked()
	return nil
}

func (a *App) rescanLocked() {
	if a.cfg.MusicRoot == "" {
		return
	}
	a.lib = ScanLibrary(a.cfg.MusicRoot)
	a.refreshMissingLocked()
}

func (a *App) refreshMissingLocked() {
	for i := range a.current {
		t := &a.current[i]
		_, err := os.Stat(t.Path)
		t.Missing = err != nil
	}
}

// ---- 设置 ----

func (a *App) GetSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Settings
}

func (a *App) UpdateSettings(s Settings) Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	s.Party = s.Party.normalize()
	s.Comp = s.Comp.normalize()
	a.cfg.Settings = s
	a.saveConfigLocked()
	return s
}

// ---- 时长缓存 ----

// PutDuration 记录浏览器探测到的音频时长（秒）。
func (a *App) PutDuration(path string, seconds float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if seconds <= 0 {
		return
	}
	a.dur[path] = seconds
	b, _ := json.MarshalIndent(a.dur, "", "  ")
	_ = os.WriteFile(a.durationsPath(), b, 0o644)
}

func (a *App) Duration(path string) float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dur[path]
}

func (a *App) durationsSnapshot() map[string]float64 {
	out := make(map[string]float64, len(a.dur))
	for k, v := range a.dur {
		out[k] = v
	}
	return out
}

// ---- 状态快照（前端单次拉取全部数据） ----

type CategoryView struct {
	Name   string  `json:"name"`
	Group  string  `json:"group"` // 摩登 / 拉丁 / 交谊舞 / 其它
	Tracks []Track `json:"tracks"`
}

type StateView struct {
	MusicRoot  string              `json:"music_root"`
	Settings   Settings            `json:"settings"`
	Categories []CategoryView      `json:"categories"`
	Playlist   []Track             `json:"playlist"`
	Playlists  []PlaylistMeta      `json:"playlists"`
	Durations  map[string]float64  `json:"durations"`
	LanURLs    []string            `json:"lan_urls,omitempty"` // 局域网接入地址（-listen 0.0.0.0 时非空），由 server 层填入
	Current    CurrentPlaylistInfo `json:"current"`
	Version    string              `json:"version,omitempty"` // 由 server 层填入
	BuildTime  string              `json:"build_time,omitempty"`
}

func (a *App) State() StateView {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := StateView{
		MusicRoot: a.cfg.MusicRoot,
		Settings:  a.cfg.Settings,
		Durations: a.durationsSnapshot(),
	}
	if a.lib != nil {
		v.Categories = a.lib.Categories()
	}
	pl := make([]Track, len(a.current))
	copy(pl, a.current)
	v.Playlist = pl
	metas, _ := a.ListPlaylistsLocked()
	v.Playlists = metas
	v.Current = CurrentPlaylistInfo{Count: len(a.current), MountedDir: a.cfg.MusicRoot}
	return v
}

// ---- 保存/读取歌单元数据列表 ----

type PlaylistMeta struct {
	Name       string    `json:"name"`
	MountedDir string    `json:"mounted_dir"` // 挂载的曲库目录（歌单固有属性）
	Count      int       `json:"count"`
	Updated    time.Time `json:"updated"`
}

func playlistFile(name string) (string, error) {
	if name == "" || name == "." || name == ".." || len(name) > 100 {
		return "", errors.New("歌单名不合法")
	}
	for _, r := range name {
		if r == '/' || r == '\\' || r == ':' || r < 0x20 {
			return "", errors.New("歌单名含非法字符")
		}
	}
	return name + ".json", nil
}

// ListPlaylistsLocked 需在持有 a.mu 时调用（只读文件名与 stat）。
func (a *App) ListPlaylistsLocked() ([]PlaylistMeta, error) {
	entries, err := os.ReadDir(a.playlistsDir())
	if err != nil {
		return nil, err
	}
	var metas []PlaylistMeta
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
		metas = append(metas, PlaylistMeta{
			Name:       trimJSONExt(e.Name()),
			MountedDir: doc.MountedDir,
			Count:      len(doc.Tracks),
			Updated:    doc.Updated,
		})
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Updated.After(metas[j].Updated) })
	return metas, nil
}

// mountedDirsLocked 返回全部歌单的挂载目录（需持有 a.mu）。
func (a *App) mountedDirsLocked() []string {
	var out []string
	metas, _ := a.ListPlaylistsLocked()
	for _, m := range metas {
		if m.MountedDir != "" {
			out = append(out, m.MountedDir)
		}
	}
	return out
}

// CurrentPlaylistInfo 当前歌单摘要（供歌单管理页展示）。
type CurrentPlaylistInfo struct {
	Count      int    `json:"count"`
	MountedDir string `json:"mounted_dir"` // 即当前音乐库根目录
}

func trimJSONExt(name string) string {
	if len(name) > 5 && name[len(name)-5:] == ".json" {
		return name[:len(name)-5]
	}
	return name
}
