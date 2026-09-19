package ui

// 「4 文件命名」页：命名项（列值 / 模板名）的增删、排序与文件名示例预览。
// 命名项按从上到下的顺序叠加组成生成文件名；开启子文件夹时，
// 其中的「列值」项叠加作为子文件夹名（模板名项不参与）。

import (
	"fmt"
	"path/filepath"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"docgen/internal/core"
)

// pageNaming 构建「4 文件命名」页。
func (ui *App) pageNaming() fyne.CanvasObject {
	ui.namingBox = container.NewVBox()
	ui.namingPreview = widget.NewLabel("")
	ui.namingPreview.Wrapping = fyne.TextWrapWord

	addColumn := widget.NewButton("添加「列值」命名项", func() { ui.addNamingItem("column") })
	addTemplate := widget.NewButton("添加「模板名」命名项", func() { ui.addNamingItem("template") })

	desc := widget.NewLabel("命名项按从上到下的顺序叠加，组成生成文件名（及子文件夹名）：「列值」取当前数据行中指定标题列的单元格值；" +
		"「模板名」取模板显示名（自动去掉序号与「模板-」前缀）。")
	desc.Wrapping = fyne.TextWrapWord

	return sectionCard("4 · 文件命名", vbox(cardGap,
		desc,
		hbox(cardGap, addColumn, addTemplate),
		ui.namingBox,
		widget.NewSeparator(),
		ui.namingPreview,
	))
}

// addNamingItem 新增一个命名项。
func (ui *App) addNamingItem(t string) {
	ui.cfg.NamingItems = append(ui.cfg.NamingItems, core.NamingItem{Type: t})
	ui.rebuildNamingList()
	ui.updateNamingPreview()
}

// rebuildNamingList 依据当前配置重建命名项列表。
func (ui *App) rebuildNamingList() {
	if ui.namingBox == nil {
		return
	}
	ui.suppress++
	defer func() { ui.suppress-- }()

	objs := make([]fyne.CanvasObject, 0, len(ui.cfg.NamingItems)+1)
	if len(ui.cfg.NamingItems) == 0 {
		objs = append(objs, widget.NewLabel("（尚未添加命名项）"))
	}
	for i := range ui.cfg.NamingItems {
		objs = append(objs, ui.namingItemRow(i))
	}
	ui.namingBox.Objects = objs
	ui.namingBox.Refresh()
}

// namingItemRow 构建第 i 个命名项的编辑行。
func (ui *App) namingItemRow(i int) fyne.CanvasObject {
	saved := ui.cfg.NamingItems[i]

	typeSel := widget.NewSelect([]string{"列值", "模板名"}, func(s string) {
		if ui.suppress > 0 || i >= len(ui.cfg.NamingItems) {
			return
		}
		t := "column"
		if s == "模板名" {
			t = "template"
		}
		if ui.cfg.NamingItems[i].Type != t {
			ui.cfg.NamingItems[i].Type = t
			ui.cfg.NamingItems[i].Key = "" // 类型切换后重新选择
			ui.rebuildNamingList()
			ui.updateNamingPreview()
		}
	})

	var mid fyne.CanvasObject
	if saved.Type == "template" {
		setSelect(typeSel, "模板名")
		mid = widget.NewLabel("取模板显示名")
	} else {
		setSelect(typeSel, "列值")
		colSel := widget.NewSelect(ui.columnOptions(), func(s string) {
			if ui.suppress > 0 || i >= len(ui.cfg.NamingItems) {
				return
			}
			ui.cfg.NamingItems[i].Key = s
			ui.updateNamingPreview()
		})
		colSel.PlaceHolder = "选择标题列"
		setSelect(colSel, saved.Key)
		mid = colSel
	}

	left := container.NewHBox(widget.NewLabel(fmt.Sprintf("%d.", i+1)), typeSel, mid)
	return container.NewBorder(nil, nil, nil,
		ui.rowButtons(func() { ui.moveNamingItem(i, -1) },
			func() { ui.moveNamingItem(i, 1) },
			func() { ui.deleteNamingItem(i) }),
		left)
}

// moveNamingItem 命名项在列表内上移/下移。
func (ui *App) moveNamingItem(i, delta int) {
	j := i + delta
	if j < 0 || j >= len(ui.cfg.NamingItems) {
		return
	}
	s := ui.cfg.NamingItems
	s[i], s[j] = s[j], s[i]
	ui.rebuildNamingList()
	ui.updateNamingPreview()
}

// deleteNamingItem 删除第 i 个命名项。
func (ui *App) deleteNamingItem(i int) {
	if i < 0 || i >= len(ui.cfg.NamingItems) {
		return
	}
	s := ui.cfg.NamingItems
	ui.cfg.NamingItems = append(s[:i], s[i+1:]...)
	ui.rebuildNamingList()
	ui.updateNamingPreview()
}

// updateNamingPreview 用第一条数据行 + 第一个可用模板，展示文件名示例。
func (ui *App) updateNamingPreview() {
	if ui.namingPreview == nil {
		return
	}
	if len(ui.cfg.NamingItems) == 0 {
		ui.namingPreview.SetText("文件名示例：（尚未添加命名项）")
		return
	}
	if ui.rows == nil || len(ui.headerNames) == 0 {
		ui.namingPreview.SetText("文件名示例：请先在「1 数据源」中加载数据")
		return
	}
	ri := -1
	for i := ui.cfg.HeaderRow; i < len(ui.rows); i++ {
		if !core.IsEmptyRow(ui.rows[i]) {
			ri = i
			break
		}
	}
	if ri < 0 {
		ui.namingPreview.SetText("文件名示例：标题行下方暂无数据行")
		return
	}
	fields := core.RowToFields(ui.rows[ri], ui.colMap)
	tplName := ui.firstTemplateName()
	base, errMsg := core.BuildFilename(ui.cfg.NamingItems, fields, tplName)
	if errMsg != "" {
		ui.namingPreview.SetText("文件名示例：" + errMsg)
		return
	}
	text := "文件名示例：" + base + filepath.Ext(tplName) +
		"（取第 " + strconv.Itoa(ri+1) + " 行数据）"
	if ui.cfg.CreateSubfolder {
		if folder, ferr := core.BuildFolderName(ui.cfg.NamingItems, fields); ferr == "" {
			text += "　·　子文件夹：" + folder
		}
	}
	ui.namingPreview.SetText(text)
}

// firstTemplateName 取第一个选了模板的规则的第一个模板名，作为命名示例；
// 都没有时使用占位示例（显示名「示例」）。
func (ui *App) firstTemplateName() string {
	for i := range ui.cfg.ContentRules {
		if n := ui.ruleFirstTemplate(ui.cfg.ContentRules[i].TemplateConfig); n != "" {
			return n
		}
	}
	for i := range ui.cfg.NumberRules {
		if n := ui.ruleFirstTemplate(ui.cfg.NumberRules[i].TemplateConfig); n != "" {
			return n
		}
	}
	return "1.模板-示例.docx"
}

// ruleFirstTemplate 取一条规则的第一个模板名；分组模式下取第一个选中分组的第一个成员。
func (ui *App) ruleFirstTemplate(tc core.TemplateConfig) string {
	if tc.TemplateMode == "group" {
		for _, g := range tc.SelectedGroups {
			if m := ui.cfg.Groups[g]; len(m) > 0 {
				return m[0]
			}
		}
		return ""
	}
	if len(tc.SelectedTemplates) > 0 {
		return tc.SelectedTemplates[0]
	}
	return ""
}
