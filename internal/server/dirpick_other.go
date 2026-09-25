//go:build !darwin && !windows && !linux

package server

import (
	"errors"
	"os/exec"
)

func pickDirectoryImpl(title string) (string, error) {
	return "", errors.New("当前系统不支持目录选择对话框，请手动输入路径")
}

func hidePickWindow(cmd *exec.Cmd) {}
