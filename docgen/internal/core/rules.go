package core

import (
	"os"
	"path/filepath"
	"strings"
)

// ---------- 规则与数据行解析 ----------

// BuildHeaderMap 构建 {列名: 列索引} 映射（重复列名取首个），并按顺序返回列名列表。
// 对应 Python 版 _build_header_map。
func BuildHeaderMap(rows [][]CellValue, headerRow int) (map[string]int, []string) {
	if headerRow < 1 || headerRow > len(rows) {
		return map[string]int{}, nil
	}
	headers := rows[headerRow-1]
	colMap := map[string]int{}
	var names []string
	for i, h := range headers {
		s := h.String()
		if s == "" {
			continue
		}
		if _, ok := colMap[s]; !ok {
			colMap[s] = i
			names = append(names, s)
		}
	}
	return colMap, names
}

// RowToFields 把一行数据转为 {列名: 单元格值}。对应 Python 版 _row_to_fields。
func RowToFields(row []CellValue, colMap map[string]int) map[string]CellValue {
	fields := make(map[string]CellValue, len(colMap))
	for name, idx := range colMap {
		if idx < len(row) {
			fields[name] = row[idx]
		} else {
			fields[name] = CellValue{}
		}
	}
	return fields
}

// IsEmptyRow 判断一行是否全空。对应 Python 版 _is_empty_row。
func IsEmptyRow(row []CellValue) bool {
	if len(row) == 0 {
		return true
	}
	for _, v := range row {
		if !v.Empty() {
			return false
		}
	}
	return true
}

// TruncateTrailingEmptyRows 截断尾部连续空行。
// 对应 Python 版 _find_last_data_row + rows[:last+1] 切片。
func TruncateTrailingEmptyRows(rows [][]CellValue) [][]CellValue {
	last := -1
	for i := len(rows) - 1; i >= 0; i-- {
		if !IsEmptyRow(rows[i]) {
			last = i
			break
		}
	}
	if last < 0 {
		return nil
	}
	return rows[:last+1]
}

// SelectContentRows 内容模式：返回标题列下内容与指定值全等的数据行索引（0 基）。
// 从标题行的下一行开始扫描到末尾，跳过空行。对应 Python 版 _select_content_rows。
func SelectContentRows(rows [][]CellValue, headerRow int, colMap map[string]int, titleColumn, contentValue string) []int {
	idx, ok := colMap[titleColumn]
	if !ok {
		return nil
	}
	target := strings.TrimSpace(contentValue)
	var result []int
	for i := headerRow; i < len(rows); i++ {
		if IsEmptyRow(rows[i]) {
			continue
		}
		var val CellValue
		if idx < len(rows[i]) {
			val = rows[i][idx]
		}
		if val.String() == target {
			result = append(result, i)
		}
	}
	return result
}

// SelectNumberRows 数字模式：返回 [rowStart, rowEnd] 行范围内非空数据行的索引（0 基）。
// 对应 Python 版 _select_number_rows。
func SelectNumberRows(rows [][]CellValue, rowStart, rowEnd int) []int {
	start := max(1, rowStart)
	end := min(len(rows), rowEnd)
	var result []int
	for i := start - 1; i < end; i++ {
		if !IsEmptyRow(rows[i]) {
			result = append(result, i)
		}
	}
	return result
}

// ResolveTemplates 解析一条规则最终要用的模板文件名列表。
// 返回 (模板名列表, 错误原因)，错误原因为空串表示正常。
// 对应 Python 版 _resolve_templates。
func ResolveTemplates(tc TemplateConfig, groups map[string][]string, defaultDir string) ([]string, string) {
	folder := strings.TrimSpace(tc.TemplateFolder)
	if folder == "" {
		folder = defaultDir
	}
	var names []string
	if tc.TemplateMode == "group" {
		for _, g := range tc.SelectedGroups {
			names = append(names, groups[g]...)
		}
		// 去重保序
		seen := map[string]bool{}
		var uniq []string
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				uniq = append(uniq, n)
			}
		}
		names = uniq
	} else {
		names = append(names, tc.SelectedTemplates...)
	}
	if len(names) == 0 {
		return nil, "未选择任何模板"
	}
	// 校验文件存在
	var missing []string
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(folder, n)); err != nil {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		if len(missing) > 5 {
			missing = missing[:5]
		}
		return names, "模板文件夹中找不到：" + strings.Join(missing, "、")
	}
	return names, ""
}
