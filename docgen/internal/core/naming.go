package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// wsCharClass 对应 Python 正则中 \s 的 Unicode 语义（含全角空格 U+3000）。
// Go 的 \s 只认 ASCII 空白，所以统一用显式字符类。
const wsCharClass = `[\t\n\v\f\r\p{Zs}]`

var (
	collapseWsRe   = regexp.MustCompile(wsCharClass + `+`)
	serialPrefixRe = regexp.MustCompile(`^` + wsCharClass + `*\d+` + wsCharClass + `*[.、\-]` + wsCharClass + `*`)
	templatePrefixRe = regexp.MustCompile(`^模[板版]` + wsCharClass + `*[-_、]` + wsCharClass + `*`)
)

// invalidCharsReplacer 替换 Windows 文件名非法字符，对应 Python 版 _INVALID_NAME_CHARS。
var invalidCharsReplacer = strings.NewReplacer(
	"<", "_", ">", "_", ":", "_", "\"", "_",
	"/", "_", "\\", "_", "|", "_", "?", "_", "*", "_",
)

// SanitizeFilename 清理 Windows 文件名非法字符。对应 Python 版 _sanitize_filename：
// 替换非法字符 → 去首尾空白 → 去尾部句点 → 折叠连续空白为单个空格。
func SanitizeFilename(name string) string {
	name = invalidCharsReplacer.Replace(name)
	name = strings.TrimSpace(name)
	name = strings.TrimRight(name, ".")
	name = collapseWsRe.ReplaceAllString(name, " ")
	return name
}

// TemplateDisplayName 从模板文件名提取「名称」部分。对应 Python 版 _template_display_name。
// 规则：去扩展名 → 去掉开头的序号前缀（如 "1." "10、"）→ 去掉「模板-」/「模版-」前缀。
// 例："1.模板-开工报告.xlsx" → "开工报告"
func TemplateDisplayName(filename string) string {
	stem := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	name := serialPrefixRe.ReplaceAllString(stem, "")
	name = templatePrefixRe.ReplaceAllString(name, "")
	name = strings.TrimSpace(name)
	if name == "" {
		return stem
	}
	return name
}

// BuildFilename 根据命名项构建文件名主体（不含扩展名）。
// 返回 (文件名主体, 错误原因)，错误原因为空串表示正常。
// 对应 Python 版 _build_filename。
func BuildFilename(items []NamingItem, fields map[string]CellValue, templateFilename string) (string, string) {
	var parts []string
	for _, item := range items {
		if item.Type == "template" {
			parts = append(parts, TemplateDisplayName(templateFilename))
		} else {
			parts = append(parts, fields[item.Key].String())
		}
	}
	name := SanitizeFilename(strings.Join(parts, ""))
	if name == "" {
		return "", "命名结果为空（请检查命名选项对应列是否有值）"
	}
	if n := utf8.RuneCountInString(name); n > 255 {
		return "", "文件名过长（" + strconv.Itoa(n) + " 字符，超过 255 上限），无法保持全名"
	}
	return name, ""
}

// BuildFolderName 根据命名项中的「列」类型项构建文件夹名（不含模板项）。
// 用于「是否新建文件夹=是」时按当前数据行的命名项值建子文件夹。
// 对应 Python 版 _build_folder_name。
func BuildFolderName(items []NamingItem, fields map[string]CellValue) (string, string) {
	var parts []string
	for _, item := range items {
		if item.Type == "template" {
			continue
		}
		parts = append(parts, fields[item.Key].String())
	}
	name := SanitizeFilename(strings.Join(parts, ""))
	if name == "" {
		return "", "文件夹命名结果为空（请检查命名选项对应列是否有值或包含列类型命名项）"
	}
	if n := utf8.RuneCountInString(name); n > 255 {
		return "", "文件夹名过长（" + strconv.Itoa(n) + " 字符，超过 255 上限）"
	}
	return name, ""
}

// ResolveOutputPath 生成最终输出路径，遇重名自动加 " (n)" 序号后缀。
// 对应 Python 版 _resolve_output_path。
func ResolveOutputPath(folder, baseName, ext string, used map[string]bool) string {
	candidate := filepath.Join(folder, baseName+ext)
	if !used[candidate] {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			used[candidate] = true
			return candidate
		}
	}
	for n := 2; n <= 9999; n++ {
		cand := filepath.Join(folder, baseName+" ("+strconv.Itoa(n)+")"+ext)
		if !used[cand] {
			if _, err := os.Stat(cand); os.IsNotExist(err) {
				used[cand] = true
				return cand
			}
		}
	}
	used[candidate] = true
	return candidate
}
