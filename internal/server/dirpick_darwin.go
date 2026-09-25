//go:build darwin

package server

import "os/exec"

// hidePickWindow 在 macOS 无需隐藏控制台窗口。
func hidePickWindow(cmd *exec.Cmd) {}

// pickDirectoryImpl 通过 osascript 弹出 Finder 风格目录选择框。
func pickDirectoryImpl(title string) (string, error) {
	return runPick("osascript", "-e",
		`POSIX path of (choose folder with prompt "`+title+`")`)
}
