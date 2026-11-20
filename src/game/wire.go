// Package game 定义 WebSocket 对局协议及其编解码
package game

import (
	"encoding/json"
	"strconv"
)

// inbound 是客户端发来的一条消息，字段按需取用
type inbound struct {
	T      string     `json:"t"`      // 消息类型
	ID     string     `json:"id"`     // 机器人标识
	Name   string     `json:"name"`   // 昵称或房间名称
	RoomID flexString `json:"roomId"` // 目标房间号
	V      bool       `json:"v"`      // 布尔设置值
	N      *float64   `json:"n"`      // 数量设置值
	X      float64    `json:"x"`      // 横向输入
	Y      float64    `json:"y"`      // 纵向输入
	Bomb   bool       `json:"bomb"`   // 是否请求放置炸弹
	Bots   []botInput `json:"bs"`     // 批量机器人输入
	Text   string     `json:"text"`   // 聊天内容
}

// botInput 是房主一次提交的一名机器人的输入意图
type botInput struct {
	ID   string  `json:"id"`   // 机器人标识
	X    float64 `json:"x"`    // 横向输入
	Y    float64 `json:"y"`    // 纵向输入
	Bomb bool    `json:"bomb"` // 是否请求放置炸弹
}

// flexString 允许房号既可以是 "1234" 也可以是 1234
type flexString string

// UnmarshalJSON 接受字符串、数字或 null 形式的房号
func (s *flexString) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = flexString(v)
		return nil
	}
	if string(b) == "null" {
		*s = ""
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*s = flexString(n.String())
	return nil
}

// String 返回房号的字符串表示
func (s flexString) String() string {
	return string(s)
}

// 下面是发给客户端的消息，t 字段决定前端怎么解析

type roomBrief struct {
	ID      string `json:"id"`      // 房间号
	Name    string `json:"name"`    // 房间名称
	Count   int    `json:"count"`   // 真人和电脑总数
	Max     int    `json:"max"`     // 对局人数上限
	Playing bool   `json:"playing"` // 是否正在对局
	Host    string `json:"host"`    // 房主昵称
}

type seatView struct {
	ID    string `json:"id"`    // 玩家或机器人标识
	Name  string `json:"name"`  // 展示名称
	Ready bool   `json:"ready"` // 是否已准备
	Seat  int    `json:"seat"`  // 座位编号
	Robot bool   `json:"robot"` // 是否为机器人
}

type roomView struct {
	ID       string     `json:"id"`       // 房间号
	Name     string     `json:"name"`     // 房间名称
	HostID   string     `json:"hostId"`   // 房主连接标识
	Playing  bool       `json:"playing"`  // 是否正在对局
	Bots     int        `json:"bots"`     // 电脑玩家数量
	BotNames []string   `json:"botNames"` // 电脑玩家名称
	Max      int        `json:"max"`      // 对局人数上限
	Maze     bool       `json:"maze"`     // 是否启用参战者局部视野
	Seats    []seatView `json:"seats"`    // 真人座位
}

type outWelcome struct {
	T    string `json:"t"`    // 消息类型
	ID   string `json:"id"`   // 服务端分配的连接标识
	Name string `json:"name"` // 已清洗的昵称
}

// outStats 是首页展示的游玩人数统计
type outStats struct {
	T     string    `json:"t"`     // 消息类型
	Stats statsView `json:"stats"` // 首页游玩人数统计
}

type outRooms struct {
	T    string      `json:"t"`    // 消息类型
	List []roomBrief `json:"list"` // 大厅房间列表
}

type outRoom struct {
	T    string   `json:"t"`    // 消息类型
	Room roomView `json:"room"` // 房间详情
}

type outStart struct {
	T         string     `json:"t"`         // 消息类型
	Seats     []seatView `json:"seats"`     // 本局参战座位
	Viewers   []seatView `json:"viewers"`   // 当前观战者
	You       string     `json:"you"`       // 接收方连接标识
	Cols      int        `json:"cols"`      // 棋盘列数
	Rows      int        `json:"rows"`      // 棋盘行数
	Theme     int        `json:"theme"`     // 当前棋局的随机主题编号
	Maze      bool       `json:"maze"`      // 是否启用玩家局部视野
	Spectator bool       `json:"spectator"` // 是否只观看不参与对局
}

// outViewers 通知对局内浏览器刷新当前观战者列表
type outViewers struct {
	T       string     `json:"t"`       // 消息类型
	Viewers []seatView `json:"viewers"` // 当前观战者
}

type outState struct {
	T string   `json:"t"` // 消息类型
	S Snapshot `json:"s"` // 对局状态
}

// actionView 是紧凑动作包中的一名玩家输入和服务端位置校准
type actionView struct {
	Seat int // 玩家座位编号
	X    int // 横向输入
	Y    int // 纵向输入
	PX   int // 横坐标百分位
	PY   int // 纵坐标百分位
}

// MarshalJSON 将动作编码为 [座位编号, 横向输入, 纵向输入, 横坐标百分位, 纵坐标百分位]
func (a actionView) MarshalJSON() ([]byte, error) {
	raw := make([]byte, 0, 24)
	raw = append(raw, '[')
	raw = strconv.AppendInt(raw, int64(a.Seat), 10)
	raw = append(raw, ',')
	raw = strconv.AppendInt(raw, int64(a.X), 10)
	raw = append(raw, ',')
	raw = strconv.AppendInt(raw, int64(a.Y), 10)
	raw = append(raw, ',')
	raw = strconv.AppendInt(raw, int64(a.PX), 10)
	raw = append(raw, ',')
	raw = strconv.AppendInt(raw, int64(a.PY), 10)
	raw = append(raw, ']')
	return raw, nil
}

// outActions 将同一服务端帧内的多个动作合并发送
type outActions struct {
	T       string       `json:"t"` // 消息类型
	Actions []actionView `json:"a"` // 合并后的动作列表
}

type outOver struct {
	T      string  `json:"t"`      // 消息类型
	Result *Result `json:"result"` // 对局结算结果
}

type outSimple struct {
	T   string `json:"t"`             // 消息类型
	Msg string `json:"msg,omitempty"` // 可展示的提示文本
}

// chatView 是一条已清洗的房间聊天内容
type chatView struct {
	Name string `json:"name"` // 发送者昵称
	Text string `json:"text"` // 已清洗的聊天内容
}

// outChat 把一条聊天消息广播给当前房间成员
type outChat struct {
	T    string   `json:"t"`    // 消息类型
	Chat chatView `json:"chat"` // 聊天内容
}

// signOf 把客户端传来的方向压成 -1 / 0 / 1
func signOf(v float64) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// itoa 将整数转换为字符串
func itoa(v int) string {
	return strconv.Itoa(v)
}
