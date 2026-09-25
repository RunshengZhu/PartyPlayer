// 目录选择：调用操作系统自带的原生目录选择对话框（零第三方依赖）。
// macOS 用 osascript (Finder 风格)，Windows 用 PowerShell FolderBrowserDialog，
// Linux 依次尝试 zenity / kdialog。服务器与浏览器同机时才能使用。
package server

import (
	"errors"
	"os/exec"
	"strings"
	"sync"
)

var pickMu sync.Mutex // 同一时刻只允许一个选择对话框

var errCanceled = errors.New("已取消选择")

// pickDirectory 阻塞弹出系统目录选择框，返回用户选择的绝对路径。
// 平台实现见 dirpick_darwin.go / dirpick_windows.go / dirpick_linux.go。
func pickDirectory(title string) (string, error) {
	pickMu.Lock()
	defer pickMu.Unlock()

	out, err := pickDirectoryImpl(title)
	if err != nil {
		if isCanceled(err) {
			return "", errCanceled
		}
		return "", err
	}
	out = strings.TrimSpace(out)
	out = strings.TrimSuffix(out, "/")
	if out == "" {
		return "", errCanceled
	}
	return out, nil
}

func runPick(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	hidePickWindow(cmd)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		// osascript 取消时错误码 (-128) 在 stderr 而非 err 信息里，合并后供 isCanceled 识别
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			err = errors.New(err.Error() + ": " + msg)
		}
	}
	return string(b), err
}

func isNotFound(err error) bool {
	var ee *exec.Error
	return errors.As(err, &ee)
}

// isCanceled 识别各平台用户主动取消。
func isCanceled(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "-128") || // AppleScript userCanceledErr
		strings.Contains(msg, "1223") || // Windows ERROR_CANCELLED
		strings.Contains(msg, "已取消")
}
