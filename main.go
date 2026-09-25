// 舞会音乐播放器 Party Player
// 零依赖单文件版本：Go 标准库 + 浏览器播放音频。
// 编译: go build -o party-player .  （详见 build.sh / README.md）
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"partyplayer/internal/app"
	"partyplayer/internal/server"
)

const Version = "0.1.0"

// 构建时由 build.sh 通过 -ldflags -X 注入；go run / 手动构建时为本地构建标记。
var (
	BuildVersion = Version + "-dev"
	BuildTime    = "unknown"
)

func main() {
	var (
		dataDir   = flag.String("data", "", "数据目录（配置/歌单存放位置，默认用户配置目录下 PartyPlayer）")
		libFlag   = flag.String("lib", "", "音乐库根目录（首个一级子目录作为舞种分类），也可启动后在界面里设置")
		port      = flag.Int("port", 8765, "HTTP 端口（被占用时自动向后顺延）")
		listen    = flag.String("listen", "127.0.0.1", "监听地址；0.0.0.0 = 允许局域网设备（手机等）访问")
		noBrowser = flag.Bool("no-browser", false, "启动后不自动打开浏览器")
		showVer   = flag.Bool("version", false, "打印版本信息后退出")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("舞会音乐播放器 %s (构建于 %s)\n", BuildVersion, BuildTime)
		return
	}

	a, err := app.Open(*dataDir)
	if err != nil {
		log.Fatalf("初始化数据目录失败: %v", err)
	}
	if *libFlag != "" {
		if err := a.SetMusicRoot(*libFlag); err != nil {
			log.Printf("设置音乐库失败: %v", err)
		}
	}

	srv := server.New(a, webAssets())
	srv.SetVersion(BuildVersion, BuildTime)

	// 局域网接入：枚举本机 IPv4 地址，供二维码 / 页面展示
	lanHost := *listen
	if lanHost == "" {
		lanHost = "127.0.0.1"
	}

	addr := fmt.Sprintf("%s:%d", lanHost, *port)
	ln, realPort, err := listenAddr(addr, *port)
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", realPort)

	var lanURLs []string
	if lanHost == "0.0.0.0" || lanHost == "::" || lanHost == "" {
		for _, ip := range lanIPv4s() {
			lanURLs = append(lanURLs, fmt.Sprintf("http://%s:%d", ip, realPort))
		}
	}
	srv.SetLanURLs(lanURLs)

	log.Printf("舞会音乐播放器 %s（构建于 %s）", BuildVersion, BuildTime)
	log.Printf("数据目录: %s", a.DataDir())
	if a.MusicRoot() != "" {
		log.Printf("音乐库: %s", a.MusicRoot())
	} else {
		log.Printf("尚未设置音乐库，请在打开的页面中设置")
	}
	log.Printf("播放页面: %s  （Ctrl+C 退出）", url)
	for _, u := range lanURLs {
		log.Printf("局域网接入: %s  （同一 Wi-Fi 下的手机可访问）", u)
	}

	if !*noBrowser {
		go openBrowser(url)
	}
	mux := http.NewServeMux()
	srv.Register(mux)
	if err := (&http.Server{Handler: mux}).Serve(ln); err != nil {
		log.Fatalf("HTTP 服务退出: %v", err)
	}
}

// listenAddr 尝试从 port 开始监听，被占用则向后顺延最多 20 个端口。
func listenAddr(addr string, port int) (net.Listener, int, error) {
	for i := 0; i < 20; i++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", hostOf(addr), port+i))
		if err == nil {
			return ln, port + i, nil
		}
	}
	return nil, 0, fmt.Errorf("端口 %d~%d 均被占用", port, port+19)
}

func hostOf(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}

// lanIPv4s 返回本机所有非回环、已启用网卡的 IPv4 地址。
func lanIPv4s() []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip4 := ipn.IP.To4(); ip4 != nil {
					out = append(out, ip4.String())
				}
			}
		}
	}
	return out
}

func openBrowser(url string) {
	time.Sleep(300 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Run()
}
