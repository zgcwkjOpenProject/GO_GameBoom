// 炸弹人服务启动入口
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bomberman/api"
	"bomberman/game"
)

// main 启动 HTTP 与 WebSocket 服务并处理优雅关闭
func main() {
	config, err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	assets, err := api.StaticFS(config.Frontend)
	if err != nil {
		log.Fatalf("加载前端资源失败: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	hub := game.NewHub()
	go hub.Run(ctx)
	srv := &http.Server{Addr: config.listenAddr(), Handler: api.NewHandler(assets, config.BasePath, hub), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("炸弹人已运行： http://%s:%d/", config.Host, config.Port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("服务退出: %v", err)
	}
	log.Print("已停止")
}

// config 保存服务监听、前端目录和可选子路由前缀
type config struct {
	Host     string `json:"host"`     // HTTP 监听主机
	Port     int    `json:"port"`     // HTTP 监听端口
	Frontend string `json:"frontend"` // 前端静态资源目录
	BasePath string `json:"basePath"` // 站点子路由前缀
}

// loadConfig 读取并校验服务启动配置
func loadConfig(file string) (config, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return config{}, err
	}
	var c config
	if err := json.Unmarshal(data, &c); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(c.Host) == "" {
		return config{}, errors.New("host 不能为空")
	}
	if c.Port < 1 || c.Port > 65535 {
		return config{}, errors.New("port 必须在 1 到 65535 之间")
	}
	if strings.TrimSpace(c.Frontend) == "" {
		return config{}, errors.New("frontend 不能为空")
	}
	basePath, err := normalizeBasePath(c.BasePath)
	if err != nil {
		return config{}, err
	}
	c.BasePath = basePath
	return c, nil
}

// listenAddr 返回 HTTP 服务使用的主机和端口地址
func (c config) listenAddr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// normalizeBasePath 将配置中的子路由整理为无结尾斜杠的绝对路径
func normalizeBasePath(basePath string) (string, error) {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" || basePath == "/" {
		return "", nil
	}
	if !strings.HasPrefix(basePath, "/") || strings.Contains(basePath, "\\") {
		return "", fmt.Errorf("basePath 必须是以 / 开头的路径: %q", basePath)
	}
	basePath = path.Clean(basePath)
	if basePath == "/" || basePath == "." {
		return "", nil
	}
	return basePath, nil
}
