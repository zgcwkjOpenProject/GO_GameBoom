// 炸弹人 HTTP 入口的静态资源缓存测试
package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"bomberman/game"
)

// TestVersionedAssetCache 检查带版本参数的静态资源可长期缓存
func TestVersionedAssetCache(t *testing.T) {
	h := spaHandler{fsys: fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")},
		"styles.css": &fstest.MapFile{Data: []byte("body{}")},
	}}
	req := httptest.NewRequest("GET", "/styles.css?v=1.0.0", nil)
	res := httptest.NewRecorder()

	h.ServeHTTP(res, req)
	if got := res.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("版本化资源缓存头不对: %q", got)
	}
}

// TestVersionFileCache 检查版本文件始终不缓存，方便浏览器发现新发布
func TestVersionFileCache(t *testing.T) {
	h := spaHandler{fsys: fstest.MapFS{
		"index.html":   &fstest.MapFile{Data: []byte("<!doctype html>")},
		"version.json": &fstest.MapFile{Data: []byte(`{"version":"v1"}`)},
	}}
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/version.json", nil))
	if got := res.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("版本文件缓存头不对: %q", got)
	}
}

// TestSubRoute 检查子路由中的页面、演示接口和资源基址
func TestSubRoute(t *testing.T) {
	assets := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html><head></head><body></body></html>")},
		"styles.css": &fstest.MapFile{Data: []byte("body{color:red}")},
	}
	h := NewHandler(fs.FS(assets), "/bomberman", game.NewHub())

	root := httptest.NewRecorder()
	h.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/bomberman/", nil))
	if root.Code != http.StatusOK || !strings.Contains(root.Body.String(), "<base href=\"/bomberman/\">") {
		t.Fatalf("子路由根页不正确: code=%d body=%q", root.Code, root.Body.String())
	}

	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/bomberman/room/123", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "<base href=\"/bomberman/\">") {
		t.Fatalf("子路由页面不正确: code=%d body=%q", page.Code, page.Body.String())
	}

	asset := httptest.NewRecorder()
	h.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/bomberman/styles.css", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != "body{color:red}" {
		t.Fatalf("子路由静态资源不正确: code=%d body=%q", asset.Code, asset.Body.String())
	}

	demo := httptest.NewRecorder()
	h.ServeHTTP(demo, httptest.NewRequest(http.MethodGet, "/bomberman/demo", nil))
	if demo.Code != http.StatusOK {
		t.Fatalf("子路由演示接口状态不正确: %d", demo.Code)
	}
}
