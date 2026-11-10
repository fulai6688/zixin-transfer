// Command zixin-transfer runs a 子欣互传 node: the embedded web UI, the session
// signalling relay and the local-network broker, all in one process.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fulai6688/zixin-transfer/internal/app"
	"github.com/fulai6688/zixin-transfer/webui"
)

// These are injected at build time (see scripts/build.ps1):
//
//	go build -ldflags "-X main.version=1.2.3 -X main.commit=abc1234 -X main.buildTime=..."
//
// The defaults keep `go run .` and `go test` working without any flags.
var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

func main() {
	var (
		port       = flag.Int("port", envInt("PORT", 8080), "listen port")
		alias      = flag.String("alias", envString("ZX_ALIAS", defaultAlias()), "device display name")
		downloads  = flag.String("downloads", envString("ZX_DOWNLOADS", defaultDownloads()), "directory for received files")
		dataDir    = flag.String("data", envString("ZX_DATA", defaultDataDir()), "directory for node state")
		noAnnounce = flag.Bool("no-announce", envBool("ZX_NO_ANNOUNCE", false), "disable local network discovery")
		open       = flag.Bool("open", envBool("ZX_OPEN", true), "open the UI in the default browser on start")
		showVer    = flag.Bool("version", false, "print version and exit")
		relay      = flag.String("relay", envString("ZX_RELAY", ""), "public URL of the signalling relay used for cross-network transfers")
		addFW      = flag.Bool("install-firewall", false, "add Windows firewall rules (asks for elevation) and exit")
		delFW      = flag.Bool("uninstall-firewall", false, "remove Windows firewall rules and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("子欣互传 %s (%s/%s) commit=%s built=%s\n", version, runtime.GOOS, runtime.GOARCH, commit, buildTime)
		return
	}

	if *addFW {
		log.SetFlags(0)
		log.Println("正在申请管理员权限以添加防火墙规则，请在 UAC 窗口点\"是\"…")
		if err := app.InstallFirewall(); err != nil {
			log.Fatalf("failed to install firewall rules: %v", err)
		}
		log.Println("防火墙规则已添加：其他设备现在可以连接本机。")
		return
	}
	if *delFW {
		log.SetFlags(0)
		if err := app.UninstallFirewall(); err != nil {
			log.Fatalf("failed to remove firewall rules: %v", err)
		}
		log.Println("防火墙规则已移除。")
		return
	}

	log.SetFlags(log.Ltime)
	log.SetPrefix("zixin ")

	cfg := app.Config{
		Port:        *port,
		DownloadDir: *downloads,
		DataDir:     *dataDir,
		Alias:       *alias,
		Platform:    runtime.GOOS,
		Version:     version,
		Commit:      commit,
		BuildTime:   buildTime,
		Announce:    !*noAnnounce,
		RelayURL:    strings.TrimRight(*relay, "/"),
	}

	srv, err := app.NewServer(cfg)
	if err != nil {
		log.Fatalf("startup failed: %v", err)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
	log.Printf("alias=%s  downloads=%s", cfg.Alias, cfg.DownloadDir)
	if cfg.RelayURL != "" {
		log.Printf("relay=%s  (跨网段取件码/信令走这里)", cfg.RelayURL)
	}
	log.Printf("open %s", url)

	if *open {
		go func() {
			// Give the listener a moment before surfacing the browser.
			waitForPort(cfg.Port)
			_ = openBrowser(url)
		}()
	}

	if err := srv.Run(webui.FS()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func waitForPort(port int) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 60; i++ {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func defaultAlias() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "子欣互传"
	}
	return h
}

func defaultDownloads() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "Downloads", "子欣互传")
	}
	return filepath.Join(home, "Downloads", "子欣互传")
}

func defaultDataDir() string {
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("LOCALAPPDATA"); appdata != "" {
			return filepath.Join(appdata, "子欣互传")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".zixin-transfer"
	}
	return filepath.Join(home, ".zixin-transfer")
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return fallback
	}
	return n
}

func envBool(key string, fallback bool) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}
