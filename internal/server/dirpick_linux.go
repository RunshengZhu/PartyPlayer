//go:build linux

package server

import (
	"errors"
	"os/exec"
)

// hidePickWindow 在 Linux 无需隐藏控制台窗口。
func hidePickWindow(cmd *exec.Cmd) {}

// pickDirectoryImpl 依次尝试 zenity / kdialog。
func pickDirectoryImpl(title string) (string, error) {
	out, err := runPick("zenity", "--file-selection", "--directory", "--title="+title)
	if err != nil && isNotFound(err) {
		out, err = runPick("kdialog", "--getexistingdirectory", "~", "--title", title)
	}
	if err != nil && isNotFound(err) {
		return "", errors.New("未找到 zenity 或 kdialog，无法弹出目录选择框，请手动输入路径")
	}
	if err != nil && out == "" {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", errCanceled // zenity/kdialog 用户取消：退出码 1 且无输出
		}
	}
	return out, err
}
