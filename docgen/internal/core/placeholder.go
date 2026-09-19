package core

import (
	"regexp"
	"strings"
)

// placeholderRe 对应 Python 版 _PLACEHOLDER_RE = r"【([^【】]+)】"
var placeholderRe = regexp.MustCompile(`【([^【】]+)】`)

// RenderText 替换文本中的 【列名】 / 【列名|日期格式】 占位符。
// 未匹配到的占位符原样保留，并把其名称收集进返回的 unmatched 列表。
// 对应 Python 版 _render_text。
func RenderText(text string, fields map[string]CellValue) (string, []string) {
	if !strings.Contains(text, "【") {
		return text, nil
	}
	var unmatched []string
	newText := placeholderRe.ReplaceAllStringFunc(text, func(m string) string {
		// m 形如 【表达式】，去掉两端括号
		inner := m[len("【") : len(m)-len("】")]
		expr := strings.TrimSpace(inner)
		name, format := expr, ""
		if i := strings.Index(expr, "|"); i >= 0 {
			name = strings.TrimSpace(expr[:i])
			format = strings.TrimSpace(expr[i+1:])
		}
		if cv, ok := fields[name]; ok {
			return applyFormat(cv, format)
		}
		unmatched = append(unmatched, name)
		return m // 原样保留
	})
	return newText, unmatched
}

// collectPlaceholderNames 从一段文本中提取占位符名称并追加进 names
// （【列名|格式】只取列名部分，去重、保序）。seen 为跨调用共享的去重表。
// 供生成前的「占位符校验」提取模板中引用的列名。
func collectPlaceholderNames(text string, seen map[string]bool, names *[]string) {
	if !strings.Contains(text, "【") {
		return
	}
	for _, m := range placeholderRe.FindAllStringSubmatch(text, -1) {
		expr := strings.TrimSpace(m[1])
		name := expr
		if i := strings.Index(expr, "|"); i >= 0 {
			name = strings.TrimSpace(expr[:i])
		}
		if name != "" && !seen[name] {
			seen[name] = true
			*names = append(*names, name)
		}
	}
}
