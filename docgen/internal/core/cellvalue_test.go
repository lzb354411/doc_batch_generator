package core

// 单元格值与日期格式化测试：CellValue.Empty / CellValue.String /
// parseDate / applyFormat。

import (
	"testing"
)

func TestCellValueEmpty(t *testing.T) {
	cases := []struct {
		name string
		v    CellValue
		want bool
	}{
		{"零值", CellValue{}, true},
		{"纯空白文本", CellValue{Str: "  \t "}, true},
		{"有文本", cv("x"), false},
		{"数值", num(0), false},
		{"日期", tm("2026-01-01"), false},
	}
	for _, c := range cases {
		if got := c.v.Empty(); got != c.want {
			t.Errorf("%s：Empty() = %v，期望 %v", c.name, got, c.want)
		}
	}
}

func TestCellValueString(t *testing.T) {
	cases := []struct {
		name string
		v    CellValue
		want string
	}{
		{"文本去除首尾空白", cv("  文字  "), "文字"},
		{"整数值浮点转整数形式", num(1001), "1001"},
		{"小数值原样输出", num(123.45), "123.45"},
		{"0.5 不写成整数", num(0.5), "0.5"},
		{"负整数", num(-3), "-3"},
		{"1e15 精确输出", num(1e15), "1000000000000000"},
		{"1e16 精确输出（f 格式不用科学计数法）", num(1e16), "10000000000000000"},
		{"纯日期转斜杠格式", tm("2026-07-05"), "2026/07/05"},
		{"含时间转斜杠格式", tmd("2026-07-05 14:30:45"), "2026/07/05 14:30:45"},
	}
	for _, c := range cases {
		if got := c.v.String(); got != c.want {
			t.Errorf("%s：String() = %q，期望 %q", c.name, got, c.want)
		}
	}
}

func TestParseDate(t *testing.T) {
	// 支持日期型单元格直通
	if _, ok := parseDate(tm("2026-07-05")); !ok {
		t.Error("IsTime 单元格应直接返回 true")
	}
	// 9 种可解析的字符串格式
	valid := []string{
		"2026-07-05", "2026/07/05", "2026.07.05", "20260705",
		"2026-07-05 12:30:45", "2026/07/05 12:30:45",
		"2026年07月05日", "2026-07", "2026/07",
	}
	for _, s := range valid {
		if _, ok := parseDate(cv(s)); !ok {
			t.Errorf("应能解析日期字符串 %q", s)
		}
	}
	// 无法解析的情况
	invalid := []string{"", "abc", "2026/07/05 12:30", "2026年7月5日"}
	for _, s := range invalid {
		if _, ok := parseDate(cv(s)); ok {
			t.Errorf("不应解析字符串 %q", s)
		}
	}
}

func TestApplyFormat(t *testing.T) {
	cases := []struct {
		name   string
		v      CellValue
		format string
		want   string
	}{
		{"空格式返回原值", tm("2026-07-05"), "", "2026/07/05"},
		{"中文年月日", tm("2026-07-05"), "YYYY年MM月DD日", "2026年07月05日"},
		{"斜杠格式", tm("2026-07-05"), "YYYY/MM/DD", "2026/07/05"},
		{"含字面字符", tm("2026-01-02"), "第MM季度", "第01季度"},
		{"YY 优先于 Y 不误匹配", tm("2026-07-05"), "YY年YY月", "26年26月"},
		{"文字单元格可格式化字符串日期", cv("2026-09-01"), "YYYY年MM月DD日", "2026年09月01日"},
		{"无法解析为日期时返回原字符串", cv("abc"), "YYYY年MM月DD日", "abc"},
		{"数值单元格返回数值字符串", num(123.45), "YYYY", "123.45"},
		{"纯文本返回去空白文本", cv("  文本  "), "YYYY", "文本"},
		{"MM/DD", tm("2026-12-31"), "MM/DD", "12/31"},
	}
	for _, c := range cases {
		if got := applyFormat(c.v, c.format); got != c.want {
			t.Errorf("%s：applyFormat(%q, %q) = %q，期望 %q", c.name, c.v.String(), c.format, got, c.want)
		}
	}
}