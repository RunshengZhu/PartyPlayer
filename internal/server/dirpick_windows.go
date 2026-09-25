//go:build windows

package server

import (
	"os/exec"
	"syscall"
)

// pickDirectoryImpl 通过 PowerShell 弹出 FolderBrowserDialog。
func pickDirectoryImpl(title string) (string, error) {
	script := `
Add-Type -AssemblyName System.Windows.Forms | Out-Null
$f = New-Object System.Windows.Forms.FolderBrowserDialog
$f.Description = '` + title + `'
$f.ShowNewFolderButton = $false
if ($f.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { $f.SelectedPath }
`
	return runPick("powershell", "-NoProfile", "-STA", "-Command", script)
}

// hidePickWindow 隐藏由 os/exec 间接拉起的控制台窗口。
func hidePickWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
