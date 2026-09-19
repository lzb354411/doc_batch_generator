package core

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// ---------- 样式信息：判定「数值 + 日期格式」的日期单元格 ----------
//
// Excel 中的日期本质是数值 + 日期数字格式（t 属性仍为 "n"），
// excelize 的 GetCellType 反映不了这一点，因此这里自行解析 xl/styles.xml，
// 判定方式与 Python 版所用的 openpyxl 一致。

// styleInfo 保存从 xl/styles.xml 与 xl/workbook.xml 解析出的判定信息。
type styleInfo struct {
	xfNumFmtIDs []int          // cellXfs 顺序对应的 numFmtId
	customFmts  map[int]string // 自定义 numFmtId → formatCode
	date1904    bool           // 是否 1904 日期系统（Mac 旧版 Excel）
}

type stylesXML struct {
	NumFmts struct {
		NumFmt []struct {
			NumFmtID   int    `xml:"numFmtId,attr"`
			FormatCode string `xml:"formatCode,attr"`
		} `xml:"numFmt"`
	} `xml:"numFmts"`
	CellXfs struct {
		Xf []struct {
			NumFmtID int `xml:"numFmtId,attr"`
		} `xml:"xf"`
	} `xml:"cellXfs"`
}

type workbookXML struct {
	WorkbookPr struct {
		Date1904 bool `xml:"date1904,attr"`
	} `xml:"workbookPr"`
}

// loadStyleInfo 从 Excel 文件中解析样式与日期系统信息。解析失败时返回可用的部分。
func loadStyleInfo(path string) *styleInfo {
	si := &styleInfo{customFmts: map[int]string{}}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return si
	}
	defer zr.Close()
	readEntry := func(name string, v interface{}) bool {
		for _, f := range zr.File {
			if f.Name != name {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return false
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				return false
			}
			return xml.Unmarshal(data, v) == nil
		}
		return false
	}
	var sx stylesXML
	if readEntry("xl/styles.xml", &sx) {
		for _, nf := range sx.NumFmts.NumFmt {
			si.customFmts[nf.NumFmtID] = nf.FormatCode
		}
		for _, xf := range sx.CellXfs.Xf {
			si.xfNumFmtIDs = append(si.xfNumFmtIDs, xf.NumFmtID)
		}
	}
	var wb workbookXML
	readEntry("xl/workbook.xml", &wb)
	si.date1904 = wb.WorkbookPr.Date1904
	return si
}

// isDateNumFmtID 判断 numFmtId 是否为日期/时间格式。
// 内置 14–22、45–47 为日期时间格式；自定义格式按格式代码启发式判定。
func (si *styleInfo) isDateNumFmtID(id int) bool {
	if (id >= 14 && id <= 22) || (id >= 45 && id <= 47) {
		return true
	}
	if code, ok := si.customFmts[id]; ok {
		return isDateFormatCode(code)
	}
	return false
}

// isDateStyle 判断单元格样式（cellXfs 索引）是否为日期格式。
func (si *styleInfo) isDateStyle(xfIndex int) bool {
	if xfIndex < 0 || xfIndex >= len(si.xfNumFmtIDs) {
		return false
	}
	return si.isDateNumFmtID(si.xfNumFmtIDs[xfIndex])
}

// isDateFormatCode 按格式代码判断是否日期/时间格式，
// 启发式与 openpyxl 的 is_date_format 一致：
// 去掉引号字面量、反斜杠转义与 [] 段（颜色/条件/经过时间）后，
// 只要还剩 d/m/h/s/y 任一字母即视为日期格式。
func isDateFormatCode(code string) bool {
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case inQuote:
			// 引号内的字面量跳过
		case c == '\\':
			i++ // 转义字符跳过
		case c == '[':
			for i < len(code) && code[i] != ']' {
				i++
			}
		default:
			b.WriteByte(c)
		}
	}
	s := strings.ToLower(b.String())
	return strings.ContainsAny(s, "dmyhs")
}

// ---------- 文本型日期识别 ----------

// fullDateTextLayouts 用于把「以文本形式输入的完整日期」识别为日期值，
// 从而与其他日期列一样统一输出 "xxxx/xx/xx" 格式。
// 只取含分隔符或中文格式的完整日期（不含裸 "20260102" 与 "2026-09" 年月），
// 避免把编号等数字串误判为日期。
var fullDateTextLayouts = []string{
	"2006-01-02", "2006/01/02", "2006.01.02", "2006年01月02日",
	"2006-01-02 15:04:05", "2006/01/02 15:04:05",
}

// parseDateText 尝试把文本解析为完整日期时间。
func parseDateText(s string) (time.Time, bool) {
	for _, layout := range fullDateTextLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ---------- 数据源读取 ----------

// SheetNames 返回 Excel 文件的全部工作表名（按文件内顺序）。
func SheetNames(path string) ([]string, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, errors.New("Excel 文件不存在：" + path)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, errors.New("打开 Excel 文件失败：" + err.Error())
	}
	defer f.Close()
	return f.GetSheetList(), nil
}

// LoadSheetRows 读取指定工作表的全部单元格，保留数值/日期类型信息。
// 行号语义与 Python 版（openpyxl iter_rows）一致：空白行同样占一行，
// 保证「标题所在行 = 第 N 行」「第 x 行至第 y 行」的行号与 Excel 中看到的一致。
func LoadSheetRows(path, sheet string) ([][]CellValue, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, errors.New("打开 Excel 文件失败：" + err.Error())
	}
	defer f.Close()
	if idx, err := f.GetSheetIndex(sheet); err != nil || idx == -1 {
		return nil, errors.New("Sheet「" + sheet + "」不存在")
	}
	si := loadStyleInfo(path)
	var rows [][]CellValue
	it, err := f.Rows(sheet)
	if err != nil {
		return nil, errors.New("读取工作表失败：" + err.Error())
	}
	rowNum := 0
	for it.Next() {
		rowNum++
		cols, err := it.Columns()
		if err != nil {
			return nil, errors.New("读取行数据失败：" + err.Error())
		}
		row := make([]CellValue, len(cols))
		for ci := range cols {
			ref, err := excelize.CoordinatesToCellName(ci+1, rowNum)
			if err != nil {
				continue
			}
			row[ci] = readCell(f, sheet, ref, si)
		}
		rows = append(rows, row)
	}
	if err := it.Close(); err != nil {
		return nil, errors.New("读取工作表失败：" + err.Error())
	}
	return rows, nil
}

// readCell 读取单元格并保留类型信息，语义对齐 Python 版（openpyxl data_only=True）：
//   - 文本 → 字符串（文本形式的完整日期识别为日期值）
//   - 数值 → 浮点（不套用千分位等数字格式）
//   - 日期格式单元格 → 时间值（支持 1900/1904 日期系统；纯时间渲染为 "HH:mm:ss"）
//   - 公式 → 缓存结果
func readCell(f *excelize.File, sheet, ref string, si *styleInfo) CellValue {
	raw, err := f.GetCellValue(sheet, ref, excelize.Options{RawCellValue: true})
	if err != nil || raw == "" {
		return CellValue{}
	}
	ct, _ := f.GetCellType(sheet, ref)
	switch ct {
	case excelize.CellTypeSharedString, excelize.CellTypeInlineString:
		// 文本形式输入的完整日期识别为日期值，统一后续输出 "xxxx/xx/xx"
		if t, ok := parseDateText(strings.TrimSpace(raw)); ok {
			return CellValue{T: t, IsTime: true}
		}
		return CellValue{Str: raw}
	case excelize.CellTypeBool:
		display, _ := f.GetCellValue(sheet, ref)
		return CellValue{Str: display}
	case excelize.CellTypeError:
		return CellValue{Str: raw}
	}
	// 数值（含公式的数值缓存结果）
	fv, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		// 公式字符串结果（t="str"）等
		return CellValue{Str: raw}
	}
	if xf, err := f.GetCellStyle(sheet, ref); err == nil && si.isDateStyle(xf) {
		if t, err := excelize.ExcelDateToTime(fv, si.date1904); err == nil {
			if fv >= 0 && fv < 1 {
				// 纯时间：openpyxl 返回 time 对象、渲染为 "HH:mm:ss"
				return CellValue{Str: t.Format("15:04:05")}
			}
			return CellValue{T: t, IsTime: true}
		}
	}
	return CellValue{Num: fv, IsNum: true}
}
