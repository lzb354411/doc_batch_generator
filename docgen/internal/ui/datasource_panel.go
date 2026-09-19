package ui

// 「1 数据源」页：选择 Excel 文件、数据 Sheet 与标题所在行，并预览数据。
// 加载成功后把行数据与标题列缓存到 App，供规则页、命名页读取。

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"docgen/internal/core"
)

// previewMaxCols 预览表格最多显示的列数（含行号列）。
const previewMaxCols = 101

// pageDataSource 构建「1 数据源」页。
func (ui *App) pageDataSource() fyne.CanvasObject {
	ui.excelEntry = widget.NewEntry()
	ui.excelEntry.Disable()
	ui.excelEntry.SetText(ui.cfg.ExcelPath)

	choose := widget.NewButton("选择 Excel 文件...", ui.chooseExcel)
	reload := widget.NewButton("重新加载", func() {
		if ui.cfg.ExcelPath == "" {
			ui.setStatus("请先选择 Excel 文件")
			return
		}
		ui.reloadExcel()
	})

	ui.sheetSelect = widget.NewSelect(nil, func(s string) {
		if ui.suppress > 0 || s == "" || s == ui.cfg.SheetName {
			return
		}
		ui.cfg.SheetName = s
		ui.loadRows()
	})
	ui.sheetSelect.PlaceHolder = "选择数据 Sheet"

	ui.headerRowEntry = widget.NewEntry()
	ui.headerRowEntry.Validator = positiveIntValidator("标题所在行")
	ui.headerRowEntry.OnChanged = func(s string) {
		if ui.suppress > 0 {
			return
		}
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= 1 {
			ui.cfg.HeaderRow = n
			ui.refreshAllAfterDataChange()
		}
	}
	ui.headerRowEntry.SetText(strconv.Itoa(ui.cfg.HeaderRow)) // SetText 不触发 OnChanged
	headerWrap := container.NewGridWrap(fyne.NewSize(90, 36), ui.headerRowEntry)

	ui.previewLabel = widget.NewLabel("尚未加载数据。选择 Excel 文件后自动加载。")
	ui.buildPreviewTable()

	return sectionCard("1 · 数据源", vbox(cardGap,
		container.NewBorder(nil, nil, hbox(cardGap, choose, reload), nil, ui.excelEntry),
		hbox(cardGap, widget.NewLabel("数据 Sheet："), ui.sheetSelect),
		hbox(cardGap, widget.NewLabel("标题所在行："), headerWrap,
			widget.NewLabel("（Excel 中的行号，从 1 开始）")),
		ui.previewLabel,
		withMinHeight(container.NewMax(ui.previewTable), 240),
	))
}

// positiveIntValidator 返回「必须为 ≥1 整数」的输入校验器。
func positiveIntValidator(name string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s不能为空", name)
		}
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err != nil || n < 1 {
			return fmt.Errorf("%s必须是大于 0 的整数", name)
		}
		return nil
	}
}

// chooseExcel 用 Windows 原生对话框选择 Excel 数据文件（.xlsx / .xlsm）。
func (ui *App) chooseExcel() {
	start := ""
	if ui.cfg.ExcelPath != "" {
		start = filepath.Dir(ui.cfg.ExcelPath)
	}
	go func() {
		path, ok := pickFile("选择 Excel 数据文件", start, []string{"xlsx", "xlsm"})
		fyne.Do(func() {
			if !ok || path == "" {
				return
			}
			ui.cfg.ExcelPath = path
			ui.excelEntry.SetText(path)
			ui.reloadExcel()
		})
	}()
}

// reloadExcel 重新读取 Excel 的 Sheet 列表，校正当前 Sheet 选择并加载数据。
func (ui *App) reloadExcel() {
	ui.suppress++
	defer func() { ui.suppress-- }()

	names, err := core.SheetNames(ui.cfg.ExcelPath)
	if err != nil {
		ui.sheetSelect.SetOptions(nil)
		ui.clearData()
		ui.setStatus("加载 Excel 失败：" + err.Error())
		return
	}
	ui.sheetSelect.SetOptions(names)
	if !containsStr(names, ui.cfg.SheetName) {
		if len(names) > 0 {
			ui.cfg.SheetName = names[0]
		} else {
			ui.cfg.SheetName = ""
		}
	}
	setSelect(ui.sheetSelect, ui.cfg.SheetName)
	ui.loadRows()
}

// loadRows 读取当前 Sheet 的全部数据行并刷新各页。
func (ui *App) loadRows() {
	if ui.cfg.SheetName == "" {
		ui.clearData()
		return
	}
	rows, err := core.LoadSheetRows(ui.cfg.ExcelPath, ui.cfg.SheetName)
	if err != nil {
		ui.clearData()
		ui.setStatus("读取数据失败：" + err.Error())
		return
	}
	ui.rows = core.TruncateTrailingEmptyRows(rows)
	ui.refreshAllAfterDataChange()
	ui.setStatus(fmt.Sprintf("已加载 %d 行数据（Sheet：%s）", len(ui.rows), ui.cfg.SheetName))
}

// columnOptions 返回标题列下拉选项（数据未加载时为空）。
func (ui *App) columnOptions() []string {
	return ui.headerNames
}

// distinctValues 返回指定标题列在数据区（标题行下方非空行）出现过的去重值，
// 用作内容规则的「内容值」下拉选项。
func (ui *App) distinctValues(column string) []string {
	idx, ok := ui.colMap[column]
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for i := ui.cfg.HeaderRow; i < len(ui.rows); i++ {
		if core.IsEmptyRow(ui.rows[i]) {
			continue
		}
		var v string
		if idx < len(ui.rows[i]) {
			v = ui.rows[i][idx].String()
		}
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
		if len(out) >= 500 {
			break
		}
	}
	return out
}

// ---------- 数据预览表格 ----------

// buildPreviewTable 创建数据预览表格：首列为行号（与 Excel 行号一致），
// 其余列为标题列。标题行（Excel 标题所在行）的内容显示在固定表头中，
// 数据区仅以加粗行号标记该行，不再重复显示标题内容。
func (ui *App) buildPreviewTable() {
	ui.previewTable = widget.NewTable(
		func() (int, int) {
			return len(ui.rows), ui.previewColCount()
		},
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			// 内容超出列宽时以省略号截断，保证只在列宽范围内显示，不溢出重叠
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(id widget.TableCellID, o fyne.CanvasObject) {
			l := o.(*widget.Label)
			isTitleRow := ui.cfg.HeaderRow >= 1 && id.Row+1 == ui.cfg.HeaderRow
			var text string
			if isTitleRow {
				// 标题行内容已在固定表头中显示，这里只保留加粗行号作为位置标记
				if id.Col == 0 {
					text = strconv.Itoa(id.Row + 1)
				}
			} else if id.Col == 0 {
				text = strconv.Itoa(id.Row + 1)
			} else if id.Row < len(ui.rows) {
				if ci := id.Col - 1; ci < len(ui.rows[id.Row]) {
					text = ui.rows[id.Row][ci].String()
				}
			}
			l.SetText(text)
			if isTitleRow {
				l.TextStyle = fyne.TextStyle{Bold: true}
			} else {
				l.TextStyle = fyne.TextStyle{}
			}
			l.Refresh()
		})
	ui.previewTable.ShowHeaderRow = true
	ui.previewTable.CreateHeader = func() fyne.CanvasObject {
		l := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		// 列名过长时也在列宽内省略，避免表头文字溢出
		l.Truncation = fyne.TextTruncateEllipsis
		return l
	}
	ui.previewTable.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		l := o.(*widget.Label)
		text := ""
		if id.Col == 0 {
			text = "行号"
		} else if id.Col-1 < len(ui.headerNames) {
			text = ui.headerNames[id.Col-1]
		}
		l.SetText(text)
		l.Refresh()
	}
	ui.previewTable.SetColumnWidth(0, 56)
}

// previewColCount 计算预览表格的总列数（行号列 + 数据列，封顶 previewMaxCols）。
func (ui *App) previewColCount() int {
	cols := len(ui.headerNames) + 1
	for _, r := range ui.rows {
		if len(r)+1 > cols {
			cols = len(r) + 1
		}
	}
	if cols > previewMaxCols {
		cols = previewMaxCols
	}
	return cols
}

// refreshPreview 刷新预览表格与信息行。
func (ui *App) refreshPreview() {
	if ui.previewTable != nil {
		cols := ui.previewColCount()
		for c := 1; c < cols; c++ {
			ui.previewTable.SetColumnWidth(c, 130)
		}
		ui.previewTable.Refresh()
	}
	if ui.previewLabel != nil {
		if ui.rows == nil {
			ui.previewLabel.SetText("尚未加载数据。选择 Excel 文件后自动加载。")
		} else {
			ui.previewLabel.SetText(fmt.Sprintf(
				"已加载 %d 行 × %d 列 · 标题行：第 %d 行（内容显示在固定表头，数据区仅以加粗行号标记）",
				len(ui.rows), ui.previewColCount()-1, ui.cfg.HeaderRow))
		}
	}
}
