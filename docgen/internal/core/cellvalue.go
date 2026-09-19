// Package core 实现文档批量生成的核心引擎：数据读取、占位符替换、
// 命名与输出路径处理、规则选行与批量生成主流程。
// 语义对齐 Python 版 doc_batch_generator.py（逐函数注释中标注了对应关系）。
package core

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// CellValue 表示从 Excel 数据表读取的一个单元格值。
// 保留数值与日期的类型信息，使 【列名|日期格式】 能做日期格式化。
type CellValue struct {
	Str    string    // 文本值
	Num    float64   // IsNum 为 true 时的数值
	IsNum  bool
	T      time.Time // IsTime 为 true 时的日期时间值
	IsTime bool
}

// Empty 返回单元格是否为空（无值或纯空白文本）。
func (c CellValue) Empty() bool {
	return !c.IsNum && !c.IsTime && strings.TrimSpace(c.Str) == ""
}

// String 将单元格值转为字符串，对应 Python 版 _cell_str：
// 文本 → 去首尾空白；整数值的浮点 → 整数形式；
// 日期时间 → "YYYY/MM/DD HH:mm:ss"（纯日期只输出 "YYYY/MM/DD"，统一斜杠分隔，
// 对应需求「日期/时间统一输出 xxxx/xx/xx 格式」；Word 等文本场景直接显示该格式）。
func (c CellValue) String() string {
	if c.IsTime {
		if c.T.Hour() == 0 && c.T.Minute() == 0 && c.T.Second() == 0 && c.T.Nanosecond() == 0 {
			return c.T.Format("2006/01/02")
		}
		return c.T.Format("2006/01/02 15:04:05")
	}
	if c.IsNum {
		if c.Num == math.Trunc(c.Num) && !math.IsInf(c.Num, 0) && math.Abs(c.Num) < 1e15 {
			return strconv.FormatInt(int64(c.Num), 10)
		}
		return strconv.FormatFloat(c.Num, 'f', -1, 64)
	}
	return strings.TrimSpace(c.Str)
}

// dateParseLayouts 与 Python 版 _parse_date 的 9 种格式一一对应。
var dateParseLayouts = []string{
	"2006-01-02", "2006/01/02", "2006.01.02", "20060102",
	"2006-01-02 15:04:05", "2006/01/02 15:04:05",
	"2006年01月02日", "2006-01", "2006/01",
}

// parseDate 尝试把单元格值解析为时间。对应 Python 版 _parse_date。
func parseDate(c CellValue) (time.Time, bool) {
	if c.IsTime {
		return c.T, true
	}
	s := c.String()
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range dateParseLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// dateFormatTokens 对应 Python 版 _DATE_FORMAT_TOKENS。
// 顺序有意义：YYYY 必须先于 YY，避免 "YYYY" 被当成两个 "YY"。
var dateFormatTokens = []struct{ Token, Layout string }{
	{"YYYY", "2006"},
	{"YY", "06"},
	{"MM", "01"},
	{"DD", "02"},
	{"HH", "15"},
	{"mm", "04"},
	{"ss", "05"},
}

// applyFormat 按自定义日期格式（YYYY/YY/MM/DD/HH/mm/ss 令牌）格式化单元格值；
// 无法解析为日期时返回原始字符串。对应 Python 版 _apply_format。
// 令牌之外的字面字符原样输出（与 Python strftime 的行为一致）。
func applyFormat(c CellValue, format string) string {
	if format == "" {
		return c.String()
	}
	t, ok := parseDate(c)
	if !ok {
		return c.String()
	}
	var b strings.Builder
	for i := 0; i < len(format); {
		matched := false
		for _, tk := range dateFormatTokens {
			if strings.HasPrefix(format[i:], tk.Token) {
				b.WriteString(t.Format(tk.Layout))
				i += len(tk.Token)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(format[i])
			i++
		}
	}
	return b.String()
}
