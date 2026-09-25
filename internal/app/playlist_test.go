package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// parseCSV 无法脱离 App 独立测试（FindByTitle 需要 a.lib），
// 这里用最小 App 实例 + 临时音乐库验证。
func TestParseCSVReference(t *testing.T) {
	dir := t.TempDir()
	genre := filepath.Join(dir, "恰恰")
	if err := os.MkdirAll(genre, 0o755); err != nil {
		t.Fatal(err)
	}
	trackPath := filepath.Join(genre, "恰恰测试01.wav")
	if err := os.WriteFile(trackPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{dur: map[string]float64{}}
	a.lib = ScanLibrary(dir)

	csvData := "#舞会音乐播放器歌单\n" +
		"#版本:1.0\n" +
		"#音乐库基准路径:" + dir + "\n" +
		"#导出时间:2026-09-19 18:00:00\n" +
		"序号,舞种,文件名,相对路径,绝对路径,备注\n" +
		"1,恰恰,恰恰测试01.wav,恰恰/恰恰测试01.wav," + trackPath + ",\n"
	// 带 BOM
	reader := strings.NewReader("\xEF\xBB\xBF" + csvData)
	tracks, err := a.parseCSV(reader)
	if err != nil {
		t.Fatalf("parseCSV 报错: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("期望解析出 1 首，实际 %d 首", len(tracks))
	}
	if tracks[0].Path != trackPath {
		t.Fatalf("路径不符: %s", tracks[0].Path)
	}
}

func TestParseM3UAndPLS(t *testing.T) {
	m3u := "#EXTM3U\n#EXTINF:-1,abc\n/foo/a.mp3\n#comment\n/bar/b.flac\n"
	tracks, err := parseM3U(strings.NewReader(m3u))
	if err != nil || len(tracks) != 2 {
		t.Fatalf("m3u: %d 首, err=%v", len(tracks), err)
	}
	pls := "[playlist]\nFile1=/foo/a.mp3\nTitle1=x\nFile2=http://example.com/x.mp3\nNumberOfEntries=2\nVersion=2\n"
	tracks, err = parsePLS(strings.NewReader(pls))
	if err != nil || len(tracks) != 1 {
		t.Fatalf("pls: %d 首, err=%v", len(tracks), err)
	}
}

func TestInterleave(t *testing.T) {
	in := []Track{
		{Path: "/a1", Category: "A"}, {Path: "/a2", Category: "A"}, {Path: "/a3", Category: "A"},
		{Path: "/b1", Category: "B"}, {Path: "/b2", Category: "B"},
	}
	out := interleave(in)
	if len(out) != 5 {
		t.Fatalf("interleave 丢了曲目: %d", len(out))
	}
	sameAdj := 0
	for i := 1; i < len(out); i++ {
		if out[i].Category == out[i-1].Category {
			sameAdj++
		}
	}
	if sameAdj > 1 {
		t.Fatalf("交错后同舞种相邻过多: %d", sameAdj)
	}
}
