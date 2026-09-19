package core

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// ReplaceXlsxPlaceholders 将 xlsx/xlsm 模板中所有工作表文本单元格里的占位符
// 替换为字段值并另存为新文件。对应 Python 版 _generate_xlsx（openpyxl 逐格替换）。
// 只改写包含【的文本单元格，其余内容（样式、公式、其他单元格）保持原样。
func ReplaceXlsxPlaceholders(templatePath, outputPath string, fields map[string]CellValue) ([]string, error) {
	f, err := excelize.OpenFile(templatePath)
	if err != nil {
		return nil, errors.New("加载模板失败：" + err.Error())
	}
	defer f.Close()
	si := loadStyleInfo(templatePath) // 模板原始样式：改动前解析，索引与 GetCellStyle 一致
	var unmatchedAll []string
	for _, sheet := range f.GetSheetList() {
		it, err := f.Rows(sheet)
		if err != nil {
			continue
		}
		rowNum := 0
		for it.Next() {
			rowNum++
			cols, err := it.Columns()
			if err != nil {
				break
			}
			for ci, v := range cols {
				if !strings.Contains(v, "【") {
					continue
				}
				ref, err := excelize.CoordinatesToCellName(ci+1, rowNum)
				if err != nil {
					continue
				}
				unmatched, werr := replaceCellValue(f, si, sheet, ref, v, fields)
				unmatchedAll = append(unmatchedAll, unmatched...)
				if werr != nil {
					_ = it.Close()
					return nil, errors.New("保存失败：" + werr.Error())
				}
			}
		}
		_ = it.Close()
	}
	if err := f.SaveAs(outputPath); err != nil {
		return nil, errors.New("保存失败：" + err.Error())
	}
	return unmatchedAll, nil
}

// wholeCellPlaceholderRe 匹配「整格恰好是一个占位符」的情况（忽略首尾空白）。
var wholeCellPlaceholderRe = regexp.MustCompile(`^【([^【】]+)】$`)

// replaceCellValue 替换并写回一个单元格的占位符：
//   - 整格恰好是单个日期占位符 → 写入真实日期值（数值型），自动适配单元格格式，
//     避免把日期写成文本（Excel 中绿色三角、无法参与日期计算的"假日期"）；
//   - 其余情况 → 按原逻辑替换为文本。
func replaceCellValue(f *excelize.File, si *styleInfo, sheet, ref, cellText string,
	fields map[string]CellValue) ([]string, error) {
	if cv, format, ok := wholeCellDatePlaceholder(cellText, fields); ok {
		return nil, writeDateCell(f, si, sheet, ref, cv, format)
	}
	newVal, unmatched := RenderText(cellText, fields)
	if newVal == cellText {
		return unmatched, nil
	}
	return unmatched, f.SetCellValue(sheet, ref, newVal)
}

// wholeCellDatePlaceholder 解析「整格恰好是单个占位符且值为日期」的场景，
// 返回 (日期值, 显式格式, 是否命中)。
func wholeCellDatePlaceholder(cellText string, fields map[string]CellValue) (CellValue, string, bool) {
	m := wholeCellPlaceholderRe.FindStringSubmatch(strings.TrimSpace(cellText))
	if m == nil {
		return CellValue{}, "", false
	}
	expr := strings.TrimSpace(m[1])
	name, format := expr, ""
	if i := strings.Index(expr, "|"); i >= 0 {
		name = strings.TrimSpace(expr[:i])
		format = strings.TrimSpace(expr[i+1:])
	}
	cv, ok := fields[name]
	if !ok || !cv.IsTime || cv.Empty() {
		return CellValue{}, "", false
	}
	return cv, format, true
}

// writeDateCell 把日期值写入单元格：先写真实日期数值，再按规则确定数字格式：
//   - 有显式格式（【列|YYYY/MM/DD】）→ 转换为 Excel 数字格式代码并应用；
//   - 无显式格式且模板单元格本身是日期格式 → 恢复模板原格式（自动匹配模板格式）；
//   - 否则套用默认 "yyyy/mm/dd"（含时间用 "yyyy/mm/dd hh:mm:ss"），
//     保证 Excel 中显示为日期而非裸序列号或文本。
// 格式转换失败时回退为格式化文本。
func writeDateCell(f *excelize.File, si *styleInfo, sheet, ref string, cv CellValue, format string) error {
	origStyle, _ := f.GetCellStyle(sheet, ref)
	fallbackText := func() error { return f.SetCellStr(sheet, ref, applyFormat(cv, format)) }

	// 写入真实日期（excelize 会先套用内置时间格式，随后恢复/覆盖为目标格式）
	if err := f.SetCellValue(sheet, ref, cv.T); err != nil {
		return err
	}
	var numFmtCode string
	switch {
	case format != "":
		code, ok := dateFormatToNumFmt(format)
		if !ok {
			return fallbackText()
		}
		numFmtCode = code
	case origStyle != 0 && si.isDateStyle(origStyle):
		// 模板单元格自身是日期格式：恢复原样式，自动匹配模板格式
		return f.SetCellStyle(sheet, ref, ref, origStyle)
	case isWholeDay(cv.T):
		numFmtCode = "yyyy/mm/dd"
	default:
		numFmtCode = "yyyy/mm/dd hh:mm:ss"
	}
	st, err := f.NewStyle(&excelize.Style{CustomNumFmt: &numFmtCode})
	if err != nil {
		return fallbackText()
	}
	return f.SetCellStyle(sheet, ref, ref, st)
}

// isWholeDay 判断时间值是否只含日期（无时间部分）。
func isWholeDay(t time.Time) bool {
	return t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0
}

// excelNumFmtReserved 在 Excel 数字格式代码中有特殊含义、需加引号的字符
// （d/m/y/h/s 为日期格式字母，其余为数字/条件/文本格式符号）。
const excelNumFmtReserved = "dmyhs#0?@*_[]\\;"

// dateFormatToNumFmt 把占位符里的日期格式（YYYY/YY/MM/DD/HH/mm/ss 令牌）
// 翻译成 Excel 数字格式代码（yyyy/yy/mm/dd/hh/mm/ss），使单元格成为真实日期
// 且显示与模板占位符要求一致。字面字符原样保留，其中字母/特殊符号加引号
// 以免被 Excel 当作格式符号。格式不含任何日期令牌、含用户引号或翻译结果
// 为空时返回 false（此时调用方回退为格式化文本，与 Word 显示保持一致）。
func dateFormatToNumFmt(format string) (string, bool) {
	var b strings.Builder
	hasToken := false
	for i := 0; i < len(format); {
		matched := false
		for _, tk := range dateFormatTokens {
			if strings.HasPrefix(format[i:], tk.Token) {
				b.WriteString(excelNumFmtCode(tk.Token))
				i += len(tk.Token)
				matched = true
				hasToken = true
				break
			}
		}
		if matched {
			continue
		}
		c := format[i]
		if c == '"' {
			return "", false
		}
		lower := c
		if lower >= 'A' && lower <= 'Z' {
			lower += 'a' - 'A'
		}
		if strings.ContainsRune(excelNumFmtReserved, rune(lower)) {
			b.WriteByte('"')
			b.WriteByte(c)
			b.WriteByte('"')
		} else {
			b.WriteByte(c)
		}
		i++
	}
	if b.Len() == 0 || !hasToken {
		return "", false
	}
	return b.String(), true
}

// excelNumFmtCode 返回单个日期令牌对应的 Excel 数字格式代码。
// MM（月）与 mm（分）同码，Excel 按位置区分。
func excelNumFmtCode(token string) string {
	switch token {
	case "YYYY":
		return "yyyy"
	case "YY":
		return "yy"
	case "MM", "mm":
		return "mm"
	case "DD":
		return "dd"
	case "HH":
		return "hh"
	case "ss":
		return "ss"
	}
	return ""
}

// ReplaceTextPlaceholders 处理 csv/txt 纯文本模板：整体读入替换后写出。
// 读取编码：先按 UTF-8，无法解码再按 GBK；写出统一 UTF-8。
// 换行处理与 Python 版文本模式一致：读取时 \r\n / \r 统一为 \n，
// Windows 写出时 \n 转为 \r\n（CSV 的 CRLF 亦为 RFC 4180 标准）。
// 对应 Python 版 _generate_text_like。
func ReplaceTextPlaceholders(templatePath, outputPath string, fields map[string]CellValue) ([]string, error) {
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, errors.New("读取模板失败：" + err.Error())
	}
	var content string
	if utf8.Valid(data) {
		content = string(data)
	} else if decoded, err2 := simplifiedchinese.GBK.NewDecoder().Bytes(data); err2 == nil {
		content = string(decoded)
	} else {
		return nil, errors.New("读取模板失败：无法以 UTF-8 或 GBK 解码")
	}
	content = normalizeNewlines(content)
	newContent, unmatched := RenderText(content, fields)
	newContent = strings.ReplaceAll(newContent, "\n", "\r\n")
	if err := os.WriteFile(outputPath, []byte(newContent), 0o644); err != nil {
		return nil, errors.New("保存失败：" + err.Error())
	}
	return unmatched, nil
}

// normalizeNewlines 将 \r\n 与孤立的 \r 统一为 \n（等价 Python 文本模式读取）。
func normalizeNewlines(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// ---------- 按扩展名分发（对应 Python 版 _generate_file） ----------

// generateFile 按模板扩展名选择对应的生成器，返回 (是否成功, 说明)。
func generateFile(templatePath, outputPath string, fields map[string]CellValue) (bool, string) {
	ext := strings.ToLower(filepath.Ext(templatePath))
	switch ext {
	case ".xlsx", ".xlsm":
		unmatched, err := ReplaceXlsxPlaceholders(templatePath, outputPath, fields)
		return resultMsg(unmatched, err)
	case ".docx":
		unmatched, err := ReplaceDocxPlaceholders(templatePath, outputPath, fields)
		return resultMsg(unmatched, err)
	case ".csv", ".txt":
		unmatched, err := ReplaceTextPlaceholders(templatePath, outputPath, fields)
		return resultMsg(unmatched, err)
	}
	return false, "不支持的模板类型：" + ext + "（支持 .xlsx/.xlsm/.docx/.csv/.txt）"
}

// resultMsg 汇总生成结果说明，对应 Python 版的 msg 拼接逻辑：
// "已生成" 或 "已生成（未匹配占位符：去重排序后的前 5 个）"。
func resultMsg(unmatched []string, err error) (bool, string) {
	if err != nil {
		return false, err.Error()
	}
	if len(unmatched) == 0 {
		return true, "已生成"
	}
	set := map[string]bool{}
	var uniq []string
	for _, u := range unmatched {
		if !set[u] {
			set[u] = true
			uniq = append(uniq, u)
		}
	}
	sort.Strings(uniq)
	if len(uniq) > 5 {
		uniq = uniq[:5]
	}
	return true, "已生成（未匹配占位符：" + strings.Join(uniq, "、") + "）"
}

// ---------- 生成前占位符提取（GUI「占位符校验」用） ----------

// TemplatePlaceholders 扫描模板文件中的全部占位符名称（去重、按出现顺序）。
// 支持与 generateFile 相同的模板类型，用于生成前与 Excel 标题列比对。
func TemplatePlaceholders(templatePath string) ([]string, error) {
	ext := strings.ToLower(filepath.Ext(templatePath))
	switch ext {
	case ".docx":
		return DocxPlaceholders(templatePath)
	case ".xlsx", ".xlsm":
		return xlsxPlaceholders(templatePath)
	case ".csv", ".txt":
		return textFilePlaceholders(templatePath)
	}
	return nil, errors.New("不支持的模板类型：" + ext + "（支持 .xlsx/.xlsm/.docx/.csv/.txt）")
}

// xlsxPlaceholders 收集 xlsx/xlsm 各工作表文本单元格中的占位符名称。
// 遍历方式与 ReplaceXlsxPlaceholders 一致，保证校验范围与实际替换范围相同。
func xlsxPlaceholders(templatePath string) ([]string, error) {
	f, err := excelize.OpenFile(templatePath)
	if err != nil {
		return nil, errors.New("加载模板失败：" + err.Error())
	}
	defer f.Close()
	seen := map[string]bool{}
	var names []string
	for _, sheet := range f.GetSheetList() {
		it, err := f.Rows(sheet)
		if err != nil {
			continue
		}
		for it.Next() {
			cols, err := it.Columns()
			if err != nil {
				break
			}
			for _, v := range cols {
				collectPlaceholderNames(v, seen, &names)
			}
		}
		_ = it.Close()
	}
	return names, nil
}

// textFilePlaceholders 收集 csv/txt 纯文本模板中的占位符名称。
// 编码探测与 ReplaceTextPlaceholders 相同（UTF-8 优先，回退 GBK）。
func textFilePlaceholders(templatePath string) ([]string, error) {
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, errors.New("读取模板失败：" + err.Error())
	}
	var content string
	if utf8.Valid(data) {
		content = string(data)
	} else if decoded, err2 := simplifiedchinese.GBK.NewDecoder().Bytes(data); err2 == nil {
		content = string(decoded)
	} else {
		return nil, errors.New("读取模板失败：无法以 UTF-8 或 GBK 解码")
	}
	seen := map[string]bool{}
	var names []string
	collectPlaceholderNames(normalizeNewlines(content), seen, &names)
	return names, nil
}
