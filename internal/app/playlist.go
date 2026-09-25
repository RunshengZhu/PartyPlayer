package app

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---- 当前歌单操作 ----

// SetPlaylist 用给定曲目整体替换当前歌单（路径可不在库内，但会标记缺失）。
func (a *App) SetPlaylist(tracks []Track) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.current = normalizeTracks(tracks)
	a.refreshMissingLocked()
	a.saveCurrentLocked()
}

func normalizeTracks(in []Track) []Track {
	out := make([]Track, 0, len(in))
	seen := map[string]bool{}
	for _, t := range in {
		if t.Path == "" || seen[t.Path] {
			continue
		}
		seen[t.Path] = true
		if t.Title == "" {
			t.Title = strings.TrimSuffix(filepath.Base(t.Path), filepath.Ext(t.Path))
		}
		out = append(out, t)
	}
	return out
}

// AddTrack 追加一首（库内路径优先，取其舞种信息）。
func (a *App) AddTrack(path string) (Track, error) {
	// FindTrack 内部会加锁，必须先调用它再获取 a.mu，否则重入死锁
	t, ok := a.FindTrack(path)
	if !ok {
		// 允许库外文件（导入的歌单可能引用库外路径）
		if st, err := os.Stat(path); err != nil || st.IsDir() {
			return Track{}, fmt.Errorf("文件不存在: %s", path)
		}
		t = Track{Path: path, Title: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.current = append(a.current, t)
	a.saveCurrentLocked()
	return t, nil
}

// MovePlaylist 移动歌单内曲目（up/down）。
func (a *App) MovePlaylist(index int, dir string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	offset := map[string]int{"up": -1, "down": 1}[dir]
	if offset == 0 {
		return errors.New("方向不合法")
	}
	j := index + offset
	if index < 0 || index >= len(a.current) || j < 0 || j >= len(a.current) {
		return errors.New("序号越界")
	}
	a.current[index], a.current[j] = a.current[j], a.current[index]
	a.saveCurrentLocked()
	return nil
}

// RemovePlaylist 删除歌单内指定曲目。
func (a *App) RemovePlaylist(index int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if index < 0 || index >= len(a.current) {
		return errors.New("序号越界")
	}
	a.current = append(a.current[:index], a.current[index+1:]...)
	a.saveCurrentLocked()
	return nil
}

// ClearPlaylist 清空当前歌单。
func (a *App) ClearPlaylist() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.current = nil
	a.saveCurrentLocked()
}

// ShufflePlaylist 随机打乱当前歌单。
func (a *App) ShufflePlaylist() {
	a.mu.Lock()
	defer a.mu.Unlock()
	rand.Shuffle(len(a.current), func(i, j int) { a.current[i], a.current[j] = a.current[j], a.current[i] })
	a.saveCurrentLocked()
}

// ---- 随机生成 ----

// GenerateRequest 按舞种数量随机生成歌单。
type GenerateRequest struct {
	Counts   map[string]int `json:"counts"`   // 舞种 -> 数量；"*" 表示全部舞种各 N 首
	Separate bool           `json:"separate"` // true=按舞种分组排列，false=交错排列
}

func (a *App) Generate(req GenerateRequest) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lib == nil || len(a.lib.cats) == 0 {
		return 0, errors.New("音乐库为空，请先设置音乐库并确认子目录中有音频文件")
	}
	var picked []Track
	for _, c := range a.lib.cats {
		n, ok := req.Counts[c.Name]
		if !ok {
			n = req.Counts["*"]
		}
		if n <= 0 {
			continue
		}
		idx := rand.Perm(len(c.Tracks))
		if n > len(idx) {
			n = len(idx)
		}
		for i := 0; i < n; i++ {
			picked = append(picked, c.Tracks[idx[i]])
		}
	}
	if len(picked) == 0 {
		return 0, errors.New("未选择任何数量，生成结果为空")
	}
	if !req.Separate {
		// 交错：把同舞种的歌尽量打散（按舞种轮转发牌）
		picked = interleave(picked)
	} else {
		// 分组：同舞种连续，舞种间随机顺序
		rand.Shuffle(len(picked), func(i, j int) { picked[i], picked[j] = picked[j], picked[i] })
		sort.SliceStable(picked, func(i, j int) bool { return picked[i].Category < picked[j].Category })
	}
	a.current = picked
	a.saveCurrentLocked()
	return len(picked), nil
}

// interleave 同舞种曲目在保持各自随机序的前提下轮转发牌，避免连续两首同舞种。
func interleave(in []Track) []Track {
	buckets := map[string][]Track{}
	var keys []string
	for _, t := range in {
		if _, ok := buckets[t.Category]; !ok {
			keys = append(keys, t.Category)
		}
		buckets[t.Category] = append(buckets[t.Category], t)
	}
	// 桶顺序随机，出牌顺序也随机
	rand.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	out := make([]Track, 0, len(in))
	rest := len(in)
	for rest > 0 {
		rand.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
		progressed := false
		for _, k := range keys {
			if len(buckets[k]) > 0 {
				out = append(out, buckets[k][0])
				buckets[k] = buckets[k][1:]
				rest--
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}
	return out
}

// ---- 歌单保存 / 读取 / 删除 ----

type savedPlaylist struct {
	Name       string    `json:"name"`
	MountedDir string    `json:"mounted_dir"` // 歌单固有属性：挂载的曲库目录
	Tracks     []Track   `json:"tracks"`
	Updated    time.Time `json:"updated"`
}

func (a *App) SavePlaylist(name string, mount string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.current) == 0 {
		return errors.New("当前歌单为空，无法保存")
	}
	if mount == "" {
		mount = a.cfg.MusicRoot
	}
	file, err := playlistFile(name)
	if err != nil {
		return err
	}
	doc := savedPlaylist{Name: name, MountedDir: mount, Tracks: a.current, Updated: time.Now()}
	b, _ := json.MarshalIndent(doc, "", "  ")
	return os.WriteFile(filepath.Join(a.playlistsDir(), file), b, 0o644)
}

// SetPlaylistMount 更新已保存歌单的挂载目录。
func (a *App) SetPlaylistMount(name, dir string) error {
	file, err := playlistFile(name)
	if err != nil {
		return err
	}
	full := filepath.Join(a.playlistsDir(), file)
	b, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("歌单不存在: %s", name)
	}
	var doc savedPlaylist
	if json.Unmarshal(b, &doc) != nil {
		return errors.New("歌单文件损坏")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if st, serr := os.Stat(abs); serr != nil || !st.IsDir() {
		return fmt.Errorf("目录不存在: %s", abs)
	}
	doc.MountedDir = abs
	doc.Updated = time.Now()
	nb, _ := json.MarshalIndent(doc, "", "  ")
	return os.WriteFile(full, nb, 0o644)
}

// LoadPlaylist 载入歌单，返回曲目数与挂载目录。
func (a *App) LoadPlaylist(name string) (int, string, error) {
	file, err := playlistFile(name)
	if err != nil {
		return 0, "", err
	}
	b, err := os.ReadFile(filepath.Join(a.playlistsDir(), file))
	if err != nil {
		return 0, "", fmt.Errorf("歌单不存在: %s", name)
	}
	var doc savedPlaylist
	if err := json.Unmarshal(b, &doc); err != nil {
		return 0, "", fmt.Errorf("歌单文件损坏: %v", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.current = normalizeTracks(doc.Tracks)
	a.refreshMissingLocked()
	a.saveCurrentLocked()
	return len(a.current), doc.MountedDir, nil
}

func (a *App) DeletePlaylist(name string) error {
	file, err := playlistFile(name)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(a.playlistsDir(), file))
	if os.IsNotExist(err) {
		return fmt.Errorf("歌单不存在: %s", name)
	}
	return err
}

// ---- 导出 ----

// ExportPlaylist 将已保存歌单（name）或当前歌单（name==""）导出为 m3u/pls/csv/json 字节流。
func (a *App) ExportPlaylist(name, format string) (data []byte, filename string, err error) {
	a.mu.Lock()
	tracks, err := a.exportTracksLocked(name)
	root := a.cfg.MusicRoot
	a.mu.Unlock()
	if err != nil {
		return nil, "", err
	}

	base := name
	if base == "" {
		base = "playlist"
	}
	switch strings.ToLower(format) {
	case "m3u", "m3u8":
		return exportM3U(tracks), base + ".m3u", nil
	case "pls":
		return exportPLS(tracks), base + ".pls", nil
	case "csv":
		return exportCSV(tracks, root), base + ".csv", nil
	case "json", "":
		b, _ := json.MarshalIndent(savedPlaylist{Name: base, Tracks: tracks, Updated: time.Now()}, "", "  ")
		return b, base + ".json", nil
	default:
		return nil, "", fmt.Errorf("不支持的格式: %s", format)
	}
}

// exportTracksLocked 需在持有 a.mu 时调用。
func (a *App) exportTracksLocked(name string) ([]Track, error) {
	if name == "" {
		return a.current, nil
	}
	file, err := playlistFile(name)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(a.playlistsDir(), file))
	if err != nil {
		return nil, fmt.Errorf("歌单不存在: %s", name)
	}
	var doc savedPlaylist
	if json.Unmarshal(b, &doc) != nil {
		return nil, errors.New("歌单文件损坏")
	}
	return doc.Tracks, nil
}

func exportM3U(tracks []Track) []byte {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	for _, t := range tracks {
		if t.Missing {
			sb.WriteString("#EXTINF:-1," + t.Title + " [缺失]\n")
		} else {
			sb.WriteString("#EXTINF:-1," + t.Category + " - " + t.Title + "\n")
		}
		sb.WriteString(t.Path + "\n")
	}
	return []byte(sb.String())
}

func exportPLS(tracks []Track) []byte {
	var sb strings.Builder
	sb.WriteString("[playlist]\n")
	for i, t := range tracks {
		fmt.Fprintf(&sb, "File%d=%s\n", i+1, t.Path)
		fmt.Fprintf(&sb, "Title%d=%s\n", i+1, t.Category+" - "+t.Title)
	}
	fmt.Fprintf(&sb, "NumberOfEntries=%d\nVersion=2\n", len(tracks))
	return []byte(sb.String())
}

// exportCSV 与参照项目（舞会音乐播放器）的 CSV 导出格式保持一致。
func exportCSV(tracks []Track, root string) []byte {
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"#舞会音乐播放器歌单"})
	_ = w.Write([]string{"#版本:1.0"})
	_ = w.Write([]string{"#音乐库基准路径:" + root})
	_ = w.Write([]string{"#导出时间:" + time.Now().Format("2006-01-02 15:04:05")})
	_ = w.Write([]string{"序号", "舞种", "文件名", "相对路径", "绝对路径", "备注"})
	for i, t := range tracks {
		rel, _ := filepath.Rel(root, t.Path)
		if strings.HasPrefix(rel, "..") {
			rel = ""
		}
		remark := ""
		if t.Missing {
			remark = "缺失"
		}
		_ = w.Write([]string{fmt.Sprint(i + 1), t.Category, filepath.Base(t.Path), rel, t.Path, remark})
	}
	w.Flush()
	return []byte(buf.String())
}

// ---- 导入 ----

// ImportPlaylist 解析上传的歌单文件（.json/.m3u/.m3u8/.pls/.csv），返回解析出的曲目数。
func (a *App) ImportPlaylist(filename string, r io.Reader) (int, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	br := bufio.NewReader(r)
	var tracks []Track
	var err error
	switch ext {
	case ".json":
		tracks, err = a.parseJSONPlaylist(br)
	case ".m3u", ".m3u8":
		tracks, err = parseM3U(br)
	case ".pls":
		tracks, err = parsePLS(br)
	case ".csv":
		tracks, err = a.parseCSV(br)
	default:
		// 尝试按 M3U 处理（多数纯文本歌单每行一个路径）
		tracks, err = parseM3U(br)
	}
	if err != nil {
		return 0, err
	}
	if len(tracks) == 0 {
		return 0, errors.New("未从文件中解析出任何曲目")
	}
	a.SetPlaylist(tracks)
	return len(tracks), nil
}

func (a *App) parseJSONPlaylist(r io.Reader) ([]Track, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var doc savedPlaylist
	if err := json.Unmarshal(b, &doc); err != nil {
		// 也接受纯数组格式
		var arr []Track
		if json.Unmarshal(b, &arr) == nil {
			return normalizeTracks(arr), nil
		}
		return nil, errors.New("JSON 歌单格式无法识别")
	}
	return normalizeTracks(doc.Tracks), nil
}

func parseM3U(r io.Reader) ([]Track, error) {
	var tracks []Track
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if u, err := url.Parse(line); err == nil && u.Scheme != "" && u.Scheme != "file" {
			continue // 跳过 http(s) 等远程条目
		}
		line = strings.TrimPrefix(line, "file://")
		tracks = append(tracks, Track{Path: line})
	}
	return normalizeTracks(tracks), sc.Err()
}

func parsePLS(r io.Reader) ([]Track, error) {
	var tracks []Track
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "File") {
			if i := strings.Index(line, "="); i >= 0 {
				p := strings.TrimSpace(line[i+1:])
				if u, err := url.Parse(p); err == nil && u.Scheme != "" && u.Scheme != "file" {
					continue // 跳过 http(s) 等远程条目
				}
				tracks = append(tracks, Track{Path: strings.TrimPrefix(p, "file://")})
			}
		}
	}
	return normalizeTracks(tracks), sc.Err()
}

// parseCSV 兼容参照项目导出的 CSV（含 # 注释头）。优先"绝对路径"列，
// 否则"音乐库基准路径 + 相对路径"，最后按文件名在音乐库中查找。
func (a *App) parseCSV(r io.Reader) ([]Track, error) {
	cr := csv.NewReader(stripBOM(r))
	cr.FieldsPerRecord = -1 // 注释行 1 列、表头/数据行 6 列，允许行内列数不同
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	var baseRoot string
	headerIdx := -1
	var tracks []Track
	for _, rec := range records {
		if len(rec) == 0 {
			continue
		}
		cell0 := strings.TrimSpace(rec[0])
		if strings.HasPrefix(cell0, "#") {
			if strings.HasPrefix(cell0, "#音乐库基准路径:") {
				baseRoot = strings.TrimPrefix(cell0, "#音乐库基准路径:")
			}
			continue
		}
		if headerIdx < 0 {
			for i, c := range rec {
				if strings.TrimSpace(c) == "绝对路径" {
					headerIdx = i
				}
			}
			if headerIdx < 0 && len(rec) >= 5 {
				headerIdx = 4 // 参照格式固定第 5 列为绝对路径
			}
			continue
		}
		// 数据行
		var path string
		if headerIdx >= 0 && headerIdx < len(rec) {
			path = strings.TrimSpace(rec[headerIdx])
		}
		if path == "" && baseRoot != "" && len(rec) >= 4 {
			rel := strings.TrimSpace(rec[3])
			if rel != "" {
				path = filepath.Join(baseRoot, rel)
			}
		}
		if path == "" || !fileExists(path) {
			// 兜底：按文件名在音乐库中查找
			var name string
			if len(rec) >= 3 {
				name = strings.TrimSpace(rec[2])
			}
			if t, ok := a.FindByTitle(name); ok {
				tracks = append(tracks, t)
				continue
			}
		}
		if path != "" {
			tracks = append(tracks, Track{Path: path})
		}
	}
	return normalizeTracks(tracks), nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// stripBOM 去掉 UTF-8 BOM（Windows/Excel 导出的 CSV 常见）。
func stripBOM(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	if b, _ := br.Peek(3); len(b) == 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		_, _ = br.Discard(3)
	}
	return br
}
