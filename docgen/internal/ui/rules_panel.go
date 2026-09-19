package ui

// 「2 数据行规则」页：内容规则（标题列 = 内容值）与数字规则（行号范围）。
// 支持新增/删除/启用禁用/上移下移，并实时显示每条规则命中的数据行数。
// 每条规则的模板在「3 模板配置」页单独选择。

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"docgen/internal/core"
)

// pageRules 构建「2 数据行规则」页。
func (ui *App) pageRules() fyne.CanvasObject {
	ui.rulesBox = container.NewVBox()

	addContent := widget.NewButton("新增内容规则", func() {
		ui.cfg.ContentRules = append(ui.cfg.ContentRules, core.ContentRule{
			Enabled:        true,
			TemplateConfig: core.TemplateConfig{TemplateMode: "all"},
		})
		ui.rebuildRulesList()
		ui.setStatus("已新增内容规则，请在下方「模板配置」中选择模板")
	})
	addNumber := widget.NewButton("新增数字规则", func() {
		ui.cfg.NumberRules = append(ui.cfg.NumberRules, core.NumberRule{
			Enabled:        true,
			RowStart:       ui.cfg.HeaderRow + 1,
			RowEnd:         ui.cfg.HeaderRow + 1,
			TemplateConfig: core.TemplateConfig{TemplateMode: "all"},
		})
		ui.rebuildRulesList()
		ui.setStatus("已新增数字规则，请在下方「模板配置」中选择模板")
	})

	desc := widget.NewLabel("规则决定哪些数据行参与生成：内容规则 = 满足「标题列下的内容 = 指定值」的数据行；" +
		"数字规则 = 一段行号范围内的非空数据行。每条规则可单独启用/禁用，模板在下方「模板配置」区块中按规则选择。")
	desc.Wrapping = fyne.TextWrapWord

	return sectionCard("2 · 数据行规则", vbox(cardGap,
		desc,
		hbox(cardGap, addContent, addNumber),
		ui.rulesBox,
	))
}

// rebuildRulesList 依据当前配置重建规则页的全部规则行。
// 数据变化（列选项变化）、规则增删移动后调用。
func (ui *App) rebuildRulesList() {
	if ui.rulesBox == nil {
		return
	}
	ui.suppress++
	defer func() { ui.suppress-- }()

	objs := make([]fyne.CanvasObject, 0, len(ui.cfg.ContentRules)+len(ui.cfg.NumberRules)+4)
	objs = append(objs, sectionLabel("内容规则"))
	if len(ui.cfg.ContentRules) == 0 {
		objs = append(objs, widget.NewLabel("（无内容规则）"))
	}
	for i := range ui.cfg.ContentRules {
		objs = append(objs, ui.contentRuleRow(i))
	}
	objs = append(objs, sectionLabel("数字规则"))
	if len(ui.cfg.NumberRules) == 0 {
		objs = append(objs, widget.NewLabel("（无数字规则）"))
	}
	for i := range ui.cfg.NumberRules {
		objs = append(objs, ui.numberRuleRow(i))
	}
	ui.rulesBox.Objects = objs
	ui.rulesBox.Refresh()
	// 规则列表已变化（增删/内容/行列），联动刷新「模板配置」区块的规则下拉选项
	ui.refreshTemplatesPage()
}

// sectionLabel 区块内的小节标题（TraeWork 风格：绿色加粗）。
func sectionLabel(text string) fyne.CanvasObject {
	return themedLabel(text, theme.ColorNamePrimary, true)
}

// contentRuleRow 构建第 i 条内容规则的编辑行。
// 注意：闭包通过下标访问 ui.cfg.ContentRules[i]，重建后旧闭包自然作废。
func (ui *App) contentRuleRow(i int) fyne.CanvasObject {
	saved := ui.cfg.ContentRules[i] // 重建时的初始状态

	matchLabel := widget.NewLabel("")

	enable := widget.NewCheck("启用", func(on bool) {
		if ui.suppress > 0 || i >= len(ui.cfg.ContentRules) {
			return
		}
		ui.cfg.ContentRules[i].Enabled = on
	})
	enable.SetChecked(saved.Enabled)

	var valSel *widget.Select // 先声明，供 colSel 的回调引用
	colSel := widget.NewSelect(ui.columnOptions(), func(s string) {
		if ui.suppress > 0 || i >= len(ui.cfg.ContentRules) {
			return
		}
		ui.cfg.ContentRules[i].TitleColumn = s
		// 列变化后重载内容值选项；原值在新列中不存在时清空
		vals := ui.distinctValues(s)
		valSel.SetOptions(vals)
		if cur := ui.cfg.ContentRules[i].ContentValue; cur != "" && !containsStr(vals, cur) {
			ui.cfg.ContentRules[i].ContentValue = ""
			valSel.ClearSelected()
		}
		setSelect(valSel, ui.cfg.ContentRules[i].ContentValue)
		ui.updateContentMatchLabel(i, matchLabel)
	})
	colSel.PlaceHolder = "选择标题列"

	valSel = widget.NewSelect(nil, func(s string) {
		if ui.suppress > 0 || i >= len(ui.cfg.ContentRules) {
			return
		}
		ui.cfg.ContentRules[i].ContentValue = s
		ui.updateContentMatchLabel(i, matchLabel)
	})
	valSel.PlaceHolder = "选择内容值"

	setSelect(colSel, saved.TitleColumn)
	valSel.SetOptions(ui.distinctValues(saved.TitleColumn))
	setSelect(valSel, saved.ContentValue)
	ui.updateContentMatchLabel(i, matchLabel)

	left := container.NewHBox(enable, colSel, widget.NewLabel("="), valSel, matchLabel)
	return container.NewBorder(nil, nil, nil,
		ui.rowButtons(func() { ui.moveContentRule(i, -1) },
			func() { ui.moveContentRule(i, 1) },
			func() { ui.deleteContentRule(i) }),
		left)
}

// numberRuleRow 构建第 i 条数字规则的编辑行。
func (ui *App) numberRuleRow(i int) fyne.CanvasObject {
	saved := ui.cfg.NumberRules[i] // 重建时的初始状态

	matchLabel := widget.NewLabel("")

	enable := widget.NewCheck("启用", func(on bool) {
		if ui.suppress > 0 || i >= len(ui.cfg.NumberRules) {
			return
		}
		ui.cfg.NumberRules[i].Enabled = on
	})
	enable.SetChecked(saved.Enabled)

	newNumEntry := func(initial int, apply func(*core.NumberRule, int)) *widget.Entry {
		e := widget.NewEntry()
		e.Validator = positiveIntValidator("行号")
		e.OnChanged = func(s string) {
			if ui.suppress > 0 || i >= len(ui.cfg.NumberRules) {
				return
			}
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= 1 {
				apply(&ui.cfg.NumberRules[i], n)
			}
			ui.updateNumberMatchLabel(i, matchLabel)
		}
		e.SetText(strconv.Itoa(initial)) // SetText 不触发 OnChanged
		return e
	}
	startEntry := newNumEntry(saved.RowStart, func(r *core.NumberRule, n int) { r.RowStart = n })
	endEntry := newNumEntry(saved.RowEnd, func(r *core.NumberRule, n int) { r.RowEnd = n })
	startEntry.PlaceHolder = "起始行"
	endEntry.PlaceHolder = "结束行"

	ui.updateNumberMatchLabel(i, matchLabel)

	startWrap := container.NewGridWrap(fyne.NewSize(80, 36), startEntry)
	endWrap := container.NewGridWrap(fyne.NewSize(80, 36), endEntry)
	left := container.NewHBox(enable, widget.NewLabel("第"), startWrap,
		widget.NewLabel("行至"), endWrap, widget.NewLabel("行"), matchLabel)
	return container.NewBorder(nil, nil, nil,
		ui.rowButtons(func() { ui.moveNumberRule(i, -1) },
			func() { ui.moveNumberRule(i, 1) },
			func() { ui.deleteNumberRule(i) }),
		left)
}

// rowButtons 规则行右侧的「上移/下移/删除」按钮组。
func (ui *App) rowButtons(up, down, del func()) fyne.CanvasObject {
	bUp := widget.NewButton("↑", up)
	bDown := widget.NewButton("↓", down)
	bDel := widget.NewButton("×", del)
	for _, b := range []*widget.Button{bUp, bDown, bDel} {
		b.Importance = widget.LowImportance
	}
	return container.NewHBox(bUp, bDown, bDel)
}

// updateContentMatchLabel 更新内容规则行尾的命中行数提示。
func (ui *App) updateContentMatchLabel(i int, l *widget.Label) {
	if l == nil || i >= len(ui.cfg.ContentRules) {
		return
	}
	r := ui.cfg.ContentRules[i]
	n := 0
	if ui.colMap != nil && r.TitleColumn != "" && r.ContentValue != "" {
		n = len(core.SelectContentRows(ui.rows, ui.cfg.HeaderRow, ui.colMap,
			r.TitleColumn, r.ContentValue))
	}
	l.SetText("→ 命中 " + strconv.Itoa(n) + " 行")
}

// updateNumberMatchLabel 更新数字规则行尾的命中行数提示。
func (ui *App) updateNumberMatchLabel(i int, l *widget.Label) {
	if l == nil || i >= len(ui.cfg.NumberRules) {
		return
	}
	r := ui.cfg.NumberRules[i]
	if r.RowStart < 1 || r.RowEnd < r.RowStart {
		l.SetText("行号范围无效")
		return
	}
	n := len(core.SelectNumberRows(ui.rows, r.RowStart, r.RowEnd))
	l.SetText("→ 命中 " + strconv.Itoa(n) + " 行")
}

// ---------- 增删移动 ----------

// moveContentRule 内容规则在列表内上移/下移。
func (ui *App) moveContentRule(i, delta int) {
	j := i + delta
	if j < 0 || j >= len(ui.cfg.ContentRules) {
		return
	}
	s := ui.cfg.ContentRules
	s[i], s[j] = s[j], s[i]
	ui.rebuildRulesList()
}

// deleteContentRule 删除第 i 条内容规则。
func (ui *App) deleteContentRule(i int) {
	if i < 0 || i >= len(ui.cfg.ContentRules) {
		return
	}
	s := ui.cfg.ContentRules
	ui.cfg.ContentRules = append(s[:i], s[i+1:]...)
	ui.rebuildRulesList()
}

// moveNumberRule 数字规则在列表内上移/下移。
func (ui *App) moveNumberRule(i, delta int) {
	j := i + delta
	if j < 0 || j >= len(ui.cfg.NumberRules) {
		return
	}
	s := ui.cfg.NumberRules
	s[i], s[j] = s[j], s[i]
	ui.rebuildRulesList()
}

// deleteNumberRule 删除第 i 条数字规则。
func (ui *App) deleteNumberRule(i int) {
	if i < 0 || i >= len(ui.cfg.NumberRules) {
		return
	}
	s := ui.cfg.NumberRules
	ui.cfg.NumberRules = append(s[:i], s[i+1:]...)
	ui.rebuildRulesList()
}
