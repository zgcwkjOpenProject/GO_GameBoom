// Package game 存放 Hub 使用的输入与数值工具
package game

import (
	"math"
	"strings"
)

// clean 清洗用户输入的文本：去掉控制字符与尖括号，再按字数截断
func clean(text string, max int) string {
	var b strings.Builder
	for _, r := range text {
		if r < 0x20 || r == 0x7f || r == '<' || r == '>' {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if rs := []rune(out); len(rs) > max {
		out = string(rs[:max])
	}
	return out
}

// roundHalfUp 和 JS 的 Math.round 一致：.5 向上取
func roundHalfUp(v float64) float64 {
	return math.Floor(v + 0.5)
}

// clampInt 将整数限制在闭区间内
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
