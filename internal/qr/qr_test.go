package qr

import (
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeDimensions(t *testing.T) {
	grid, err := Encode("http://192.168.1.5:8765")
	if err != nil {
		t.Fatalf("Encode 失败: %v", err)
	}
	size := len(grid)
	if size != 25 && size != 29 { // v2=25, v3=29
		t.Fatalf("尺寸异常: %d", size)
	}
	for _, row := range grid {
		if len(row) != size {
			t.Fatal("矩阵不是方阵")
		}
	}
	// 三个定位图案中心必须为黑
	if !grid[3][3] || !grid[3][size-4] || !grid[size-4][3] {
		t.Fatal("定位图案中心缺失")
	}
}

func TestEncodeTooLong(t *testing.T) {
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := Encode(string(long)); err == nil {
		t.Fatal("超长文本应报错")
	}
}

// TestDecodeWithOpenCV 生成 PNG 后用 OpenCV 解码验证（开发机验证，非 CI 依赖）。
// 需要: pip install opencv-python；跳过条件：无 python/cv2。
func TestDecodeWithOpenCV(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("无 python3，跳过解码验证")
	}
	tmp := t.TempDir()
	texts := []string{
		"http://192.168.1.5:8765",
		"http://192.168.100.100:8765/",
		"https://example.com/party?x=1&y=汉字",
	}
	for _, text := range texts {
		img, err := PNG(text, 8)
		if err != nil {
			t.Fatalf("PNG(%q): %v", text, err)
		}
		path := filepath.Join(tmp, "qr.png")
		f, _ := os.Create(path)
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
		cmd := exec.Command("python3", "-c", `
import sys, cv2
img = cv2.imread(sys.argv[1])
data, _, _ = cv2.QRCodeDetector().detectAndDecode(img)
print(data)
`, path)
		out, err := cmd.Output()
		if err != nil {
			t.Skipf("cv2 不可用，跳过: %v", err)
		}
		if got := strings.TrimSpace(string(out)); got != text {
			t.Fatalf("解码不符:\n got  = %q\n want = %q", got, text)
		}
	}
}
