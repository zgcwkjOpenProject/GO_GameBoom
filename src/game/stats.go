// Package game 持久化网站游玩人数统计
package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// playerStats 是写入 JSON 的总游玩与当日游玩统计
type playerStats struct {
	Date  string `json:"date"`  // 今日统计对应的本地日期
	Total int    `json:"total"` // 累计进入对局的真人次数
	Today int    `json:"today"` // 当日进入对局的真人次数

	path string // 统计 JSON 文件路径
}

// statsView 是发送给首页的公开统计数据
type statsView struct {
	Total int `json:"total"` // 累计游玩次数
	Today int `json:"today"` // 当日游玩次数
}

// newPlayerStats 从 JSON 读取人数统计，不存在或不可用时从零开始
func newPlayerStats(path string) *playerStats {
	s := &playerStats{path: path}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, s)
	}
	s.resetDay(time.Now())
	return s
}

// resetDay 在日期变化后清零今日游玩人数
func (s *playerStats) resetDay(now time.Time) {
	date := now.Format("2006-01-02")
	if s.Date == date {
		return
	}
	s.Date = date
	s.Today = 0
}

// recordGame 按进入正式对局的真人数量累计统计，不对玩家做去重
func (s *playerStats) recordGame(players int, now time.Time) {
	if players <= 0 {
		return
	}
	s.resetDay(now)
	s.Total += players
	s.Today += players
	s.save()
}

// save 原子替换统计文件，避免中断时留下半截 JSON
func (s *playerStats) save() {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err == nil {
		_ = os.Rename(tmp, s.path)
	}
}

// view 返回可公开展示的总游玩人数与今日游玩人数
func (s *playerStats) view() statsView {
	return statsView{Total: s.Total, Today: s.Today}
}

// defaultStatsPath 返回程序运行目录下的统计文件位置
func defaultStatsPath() string {
	return filepath.Join("data", "players.json")
}
