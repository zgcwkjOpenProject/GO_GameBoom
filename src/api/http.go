// 炸弹人 HTTP API 与静态资源处理
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	"bomberman/game"
)

// NewHandler 按配置的子路由注册 HTTP、WebSocket 与前端静态资源
func NewHandler(assets fs.FS, basePath string, hub *game.Hub) http.Handler {
	mux := http.NewServeMux()
	wsPath := basePath + "/ws"
	demoPath := basePath + "/demo"
	mux.HandleFunc(wsPath, hub.ServeWS)
	mux.HandleFunc(demoPath, demoHandler)
	static := spaHandler{fsys: assets, basePath: basePath}
	if basePath == "" {
		mux.Handle("/", static)
		return mux
	}
	mux.HandleFunc(basePath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, basePath+"/", http.StatusMovedPermanently)
	})
	mux.Handle(basePath+"/", http.StripPrefix(basePath, static))
	return mux
}

// demoHandler 按真实开局规则生成首页演示用的随机地图与两名机器人初始状态
func demoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "方法不支持", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(game.DemoSnapshot()); err != nil {
		log.Printf("生成首页演示失败: %v", err)
	}
}

// StaticFS 从磁盘打开前端目录，不把网页资源嵌入服务端二进制文件
func StaticFS(dir string) (fs.FS, error) {
	if _, err := os.Stat(path.Join(dir, "index.html")); err != nil {
		return nil, err
	}
	// log.Printf("前端资源来自磁盘目录 %s", dir)
	return os.DirFS(dir), nil
}

// spaHandler 提供静态文件，找不到的路径回落到首页
type spaHandler struct {
	fsys     fs.FS  // fsys 是前端静态资源文件系统
	basePath string // basePath 是页面静态资源的部署前缀
}

// ServeHTTP 提供前端静态资源，并让未知路径回落到首页
func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "方法不支持", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	if name == "index.html" {
		h.serveIndex(w, r)
		return
	}
	f, err := h.fsys.Open(name)
	if err != nil {
		h.serveIndex(w, r)
		return
	}
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		_ = f.Close()
		h.serveIndex(w, r)
		return
	}
	defer f.Close()
	// HTML 与版本文件始终不缓存，带版本号的资源长期缓存，其余资源最多缓存一天
	if name == "index.html" || name == "version.json" {
		w.Header().Set("Cache-Control", "no-store")
	} else if r.URL.Query().Has("v") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "读取失败", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), rs)
}

// serveIndex 读取首页，用于前端路由回退
func (h spaHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(h.fsys, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data = bytes.Replace(data, []byte("<head>"), []byte("<head><base href=\""+h.basePath+"/\">"), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}
