// 服务启动配置测试
package main

import (
	"os"
	"testing"
)

// TestNormalizeBasePath 检查子路由前缀的规范化规则
func TestNormalizeBasePath(t *testing.T) {
	got, err := normalizeBasePath(" /game/ ")
	if err != nil || got != "/game" {
		t.Fatalf("规范化结果不正确: got=%q err=%v", got, err)
	}
	if _, err := normalizeBasePath("game"); err == nil {
		t.Fatal("相对 basePath 应被拒绝")
	}
}

// TestLoadConfig 检查 host、port 和 frontend 配置可生成监听地址
func TestLoadConfig(t *testing.T) {
	file := t.TempDir() + "/config.json"
	data := []byte(`{"host":"127.0.0.1","port":5173,"frontend":"frontend","basePath":"/game/"}`)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	if c.Frontend != "frontend" || c.BasePath != "/game" || c.listenAddr() != "127.0.0.1:5173" {
		t.Fatalf("配置解析不正确: %+v, addr=%q", c, c.listenAddr())
	}
}
