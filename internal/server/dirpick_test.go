package server

import "testing"

func TestIsCanceled(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"exit status 1: execution error: User canceled. (-128)", true}, // macOS 取消
		{"exit status 1", false}, // zenity 其他失败
		{"signal: killed", false},
		{"已取消选择", true},
		{"exit status 1223", true}, // Windows 取消
	}
	for _, c := range cases {
		if got := isCanceled(errString(c.msg)); got != c.want {
			t.Errorf("isCanceled(%q) = %v, 期望 %v", c.msg, got, c.want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
