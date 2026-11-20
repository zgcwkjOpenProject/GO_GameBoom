// Package game 实现 WebSocket 连接与房间对局服务
package game

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second // 单条消息的写超时
	pongWait       = 60 * time.Second // 超过这么久没收到任何帧就认为掉线
	pingPeriod     = 25 * time.Second // 心跳间隔
	maxMessageSize = 4096             // 客户端消息上限
	sendBuffer     = 64               // 每个连接的发送缓冲
)

// Client 是一条 WebSocket 连接。读写各由一个 goroutine 负责，
// 所有状态变更都交给 Hub 的单循环处理，因此这里不需要额外加锁。
type Client struct {
	id     string // Hub 分配的连接标识
	name   string // 已清洗的昵称
	roomID string // 当前所在房间号

	hub  *Hub            // 唯一拥有房间状态的 Hub
	conn *websocket.Conn // 此客户端的 WebSocket 连接
	out  chan []byte     // 由 Hub 写入、写协程读取的发送队列
	done chan struct{}   // 连接结束通知
	once sync.Once       // 确保底层连接只关闭一次
}

// push 把已经序列化好的消息塞进发送队列，队列满就断开这个跟不上的连接
func (c *Client) push(raw []byte) {
	if c == nil {
		return
	}
	select {
	case c.out <- raw:
	default:
		c.close()
	}
}

// close 幂等关闭连接及其完成信号
func (c *Client) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

// readPump 收消息、解析、丢给 Hub
func (c *Client) readPump() {
	defer func() {
		c.close()
		select {
		case c.hub.unregister <- c:
		case <-c.hub.done:
		}
	}()
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg inbound
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		select {
		case c.hub.commands <- command{client: c, msg: msg}:
		case <-c.done:
			return
		case <-c.hub.done:
			return
		}
	}
}

// writePump 串行地写连接，顺带发心跳
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.close()
	}()
	for {
		select {
		case raw := <-c.out:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, raw); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:    1024,
	WriteBufferSize:   4096,
	CheckOrigin:       sameOrigin,
	EnableCompression: true,
}

// sameOrigin 只放行同源页面发起的握手，没有 Origin 头的（脚本、原生客户端）也放行
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// ServeWS 处理 /ws 的握手
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &Client{hub: h, conn: conn, out: make(chan []byte, sendBuffer), done: make(chan struct{})}
	select {
	case h.register <- c:
	case <-h.done:
		c.close()
		return
	}
	go c.writePump()
	go c.readPump()
}
