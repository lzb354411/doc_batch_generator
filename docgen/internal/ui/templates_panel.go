package ui

// 「3 模板配置」页：为每条数据行规则选择模板文件夹与模板文件（多选）。
// 模板列表来自文件夹扫描（.docx/.xlsx/.xlsm/.csv/.txt），
// 勾选结果写回规则的 SelectedTemplates（保留不在当前文件夹中的既有选择）。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"docgen/internal/core"
)

// 规则类别标识（模板页的规则选择器用）。
const (
	kindContent = iota // 内容规则
	kindNumber         // 数字规则
)

// ruleKey 模板页规则选择器中一项规则的位置。
type ruleKey struct {
	kind int
	idx  int
}

// templateExts 支持的模板扩展名（与 core 的 generateFile 一致）。
var templateExts = map[string]bool{
	".docx": true, ".xlsx": true, ".xlsm": true, ".csv": true, ".txt": true,
}

// pageTemplates 构建「3 模板配置」页。
func (ui *App) pageTemplates() fyne.CanvasObject {
	ui.tplSelKind, ui.tplSelIdx = -1, -1

	ui.tplRuleSelect = widget.NewSelect(nil, func(string) {
		if ui.suppress > 0 {
			return
		}
		k := ui.tplRuleSelect.SelectedIndex()
		if k >= 0 && k < len(ui.tplRuleKeys) {
			ui.tplSelKind = ui.tplRuleKeys[k].kind
			ui.tplSelIdx = ui.tplRuleKeys[k].idx
			ui.showTplRule()
		}
	})
	ui.tplRuleSelect.PlaceHolder = "选择要配置模板的规则"

	ui.tplFolderEntry = widget.NewEntry()
	ui.tplFolderEntry.PlaceHolder = "未选择（可点击右侧按钮选择，也可直接输入路径）"
	// 路径框可编辑：手动输入路径同样生效，便于修正细小路径差异
	ui.tplFolderEntry.OnChanged = func(s string) {
		if ui.suppress > 0 {
			return
		}
		tc := ui.currentTplConfig()
		if tc == nil {
			return
		}
		tc.TemplateFolder = s
		ui.rebuildTplChecks()
	}
	choose := widget.NewButton("选择模板文件夹...", ui.chooseTplFolder)

	selAll := widget.NewButton("全选", func() { ui.setAllTplChecked(true) })
	selNone := widget.NewButton("全不选", func() { ui.setAllTplChecked(false) })
	selInv := widget.NewButton("反选", ui.invertTplChecked)

	// 模板选择模式：按勾选模板（手动选择具体文件）或按模板分组（选择分组，自动包含组内模板）
	ui.tplModeRadio = widget.NewRadioGroup([]string{"按勾选模板", "按模板分组"}, func(s string) {
		if ui.suppress > 0 {
			return
		}
		tc := ui.currentTplConfig()
		if tc == nil {
			return
		}
		mode := "all"
		if s == "按模板分组" {
			mode = "group"
		}
		if tc.TemplateMode != mode {
			tc.TemplateMode = mode
			ui.rebuildTplChecks()
		}
	})
	ui.tplModeRadio.Horizontal = true

	manageGroupBtn := widget.NewButton("管理分组...", ui.showGroupManage)

	ui.tplChecksBox = container.NewVBox()

	return sectionCard("3 · 模板配置", vbox(cardGap,
		container.NewBorder(nil, nil, widget.NewLabel("规则："), nil, ui.tplRuleSelect),
		container.NewBorder(nil, nil, widget.NewLabel("模板文件夹："), choose, ui.tplFolderEntry),
		hbox(cardGap, widget.NewLabel("模板选择："), ui.tplModeRadio, manageGroupBtn),
		hbox(cardGap, selAll, selNone, selInv,
			widget.NewLabel("（支持 .docx / .xlsx / .xlsm / .csv / .txt）")),
		ui.placeholderHint(),
		ui.tplChecksBox,
	))
}

// placeholderHint 占位符写法提示（灰色文字的提示行）。
func (ui *App) placeholderHint() fyne.CanvasObject {
	return themedLabel("模板中的占位符写法：【列名】或【列名|YYYY-MM-DD】",
		theme.ColorNamePlaceHolder, false)
}

// refreshTemplatesPage 重建规则下拉选项并展示当前选中规则的模板配置。
// 启动恢复、规则列表变化后调用（机制：rebuildRulesList 内部联动刷新）。
func (ui *App) refreshTemplatesPage() {
	if ui.tplRuleSelect == nil {
		return
	}
	ui.suppress++
	defer func() { ui.suppress-- }()

	var labels []string
	var keys []ruleKey
	for i, r := range ui.cfg.ContentRules {
		labels = append(labels, fmt.Sprintf("内容规则#%d（%s = %s）",
			i+1, orUnset(r.TitleColumn), orUnset(r.ContentValue)))
		keys = append(keys, ruleKey{kindContent, i})
	}
	for i, r := range ui.cfg.NumberRules {
		labels = append(labels, fmt.Sprintf("数字规则#%d（第 %d-%d 行）",
			i+1, r.RowStart, r.RowEnd))
		keys = append(keys, ruleKey{kindNumber, i})
	}
	ui.tplRuleKeys = keys
	ui.tplRuleSelect.SetOptions(labels)

	// 校正当前选择：失效时回到第一条规则
	valid := false
	for _, k := range keys {
		if k.kind == ui.tplSelKind && k.idx == ui.tplSelIdx {
			valid = true
			break
		}
	}
	if !valid {
		if len(keys) > 0 {
			ui.tplSelKind, ui.tplSelIdx = keys[0].kind, keys[0].idx
		} else {
			ui.tplSelKind, ui.tplSelIdx = -1, -1
		}
	}
	if len(keys) > 0 {
		for k, key := range keys {
			if key.kind == ui.tplSelKind && key.idx == ui.tplSelIdx {
				ui.tplRuleSelect.SetSelectedIndex(k)
				break
			}
		}
	} else {
		ui.tplRuleSelect.ClearSelected()
	}
	ui.showTplRule()
}

// orUnset 空字符串显示为「未选择」。
func orUnset(s string) string {
	if strings.TrimSpace(s) == "" {
		return "未选择"
	}
	return s
}

// currentTplConfig 返回模板页当前选中规则的模板配置指针，未选中时为 nil。
// 注意：规则切片扩容后指针会失效，跨时点的操作需重新调用本函数解析。
func (ui *App) currentTplConfig() *core.TemplateConfig {
	switch ui.tplSelKind {
	case kindContent:
		if ui.tplSelIdx >= 0 && ui.tplSelIdx < len(ui.cfg.ContentRules) {
			return &ui.cfg.ContentRules[ui.tplSelIdx].TemplateConfig
		}
	case kindNumber:
		if ui.tplSelIdx >= 0 && ui.tplSelIdx < len(ui.cfg.NumberRules) {
			return &ui.cfg.NumberRules[ui.tplSelIdx].TemplateConfig
		}
	}
	return nil
}

// showTplRule 展示当前选中规则的模板文件夹与模板勾选列表。
func (ui *App) showTplRule() {
	if ui.tplChecksBox == nil {
		return
	}
	tc := ui.currentTplConfig()
	if tc == nil {
		ui.tplFolderEntry.SetText("")
		ui.tplModeRadio.SetSelected("")
		ui.tplFiles = nil
		ui.tplChecks = nil
		ui.tplChecksBox.Objects = []fyne.CanvasObject{
			widget.NewLabel("请先在「2 数据行规则」中新增规则，再回到此页配置模板。"),
		}
		ui.tplChecksBox.Refresh()
		return
	}
	ui.tplFolderEntry.SetText(tc.TemplateFolder)
	if tc.TemplateMode == "group" {
		ui.tplModeRadio.SetSelected("按模板分组")
	} else {
		ui.tplModeRadio.SetSelected("按勾选模板")
	}
	// 隐藏状态下（启动恢复阶段）的 SetText 可能不触发重绘，且再次进入
	// 页面时值未变化不会刷新——这里显式重绘保证路径可见
	ui.tplFolderEntry.Refresh()
	ui.rebuildTplChecks()
}

// rebuildTplChecks 重建勾选列表：按勾选模板模式列出文件夹中的模板文件，
// 按模板分组模式列出全部分组（勾选即选用该分组）。
func (ui *App) rebuildTplChecks() {
	tc := ui.currentTplConfig()
	if tc == nil || ui.tplChecksBox == nil {
		return
	}
	ui.suppress++
	defer func() { ui.suppress-- }()

	ui.tplFiles = nil
	ui.tplChecks = nil
	objs := make([]fyne.CanvasObject, 0, 16)

	if tc.TemplateMode == "group" {
		for _, g := range ui.sortedGroupNames() {
			g := g
			cb := widget.NewCheck(fmt.Sprintf("%s（%d 个模板）", g, len(ui.cfg.Groups[g])), func(on bool) {
				if ui.suppress > 0 {
					return
				}
				ui.toggleGroup(g, on)
			})
			cb.SetChecked(containsStr(tc.SelectedGroups, g))
			ui.tplChecks = append(ui.tplChecks, cb)
			objs = append(objs, cb)
		}
		if len(ui.cfg.Groups) == 0 {
			objs = append(objs, widget.NewLabel("（尚未创建分组，点击「管理分组...」创建）"))
		}
	} else {
		files := scanTemplateFiles(tc.TemplateFolder)
		ui.tplFiles = files
		for _, f := range files {
			f := f
			cb := widget.NewCheck(f, func(on bool) {
				if ui.suppress > 0 {
					return
				}
				ui.toggleTpl(f, on)
			})
			cb.SetChecked(containsStr(tc.SelectedTemplates, f))
			ui.tplChecks = append(ui.tplChecks, cb)
			objs = append(objs, cb)
		}
		if len(files) == 0 {
			objs = append(objs, widget.NewLabel("（模板文件夹为空或不存在）"))
		}
	}
	ui.tplChecksBox.Objects = objs
	ui.tplChecksBox.Refresh()
}

// sortedGroupNames 返回按名称排序的分组名列表。
func (ui *App) sortedGroupNames() []string {
	names := make([]string, 0, len(ui.cfg.Groups))
	for g := range ui.cfg.Groups {
		names = append(names, g)
	}
	sort.Strings(names)
	return names
}

// toggleGroup 勾选/取消勾选一个模板分组（写入当前规则的 SelectedGroups）。
func (ui *App) toggleGroup(name string, on bool) {
	tc := ui.currentTplConfig()
	if tc == nil {
		return
	}
	if on {
		if !containsStr(tc.SelectedGroups, name) {
			tc.SelectedGroups = append(tc.SelectedGroups, name)
		}
		return
	}
	sel := tc.SelectedGroups
	out := sel[:0]
	for _, n := range sel {
		if n != name {
			out = append(out, n)
		}
	}
	tc.SelectedGroups = out
}

// toggleTpl 勾选/取消勾选一个模板。
// 仅增删该模板本身，保留不在当前文件夹中的既有选择（切回文件夹后自动恢复勾选）。
func (ui *App) toggleTpl(name string, on bool) {
	tc := ui.currentTplConfig()
	if tc == nil {
		return
	}
	if on {
		if !containsStr(tc.SelectedTemplates, name) {
			tc.SelectedTemplates = append(tc.SelectedTemplates, name)
		}
		return
	}
	sel := tc.SelectedTemplates
	out := sel[:0]
	for _, n := range sel {
		if n != name {
			out = append(out, n)
		}
	}
	tc.SelectedTemplates = out
}

// setAllTplChecked 全选/全不选当前文件夹中的模板。
func (ui *App) setAllTplChecked(on bool) {
	for _, cb := range ui.tplChecks {
		cb.SetChecked(on) // 值变化才触发回调，逐个写回配置
	}
}

// invertTplChecked 反选当前文件夹中的模板。
func (ui *App) invertTplChecked() {
	for _, cb := range ui.tplChecks {
		cb.SetChecked(!cb.Checked)
	}
}

// chooseTplFolder 用 Windows 原生对话框为当前选中规则选择模板文件夹。
func (ui *App) chooseTplFolder() {
	if ui.currentTplConfig() == nil {
		ui.setStatus("请先选择要配置模板的规则")
		return
	}
	go func() {
		path, ok := pickDir("选择模板文件夹", ui.tplFolderEntry.Text)
		fyne.Do(func() {
			if !ok {
				return
			}
			tc := ui.currentTplConfig() // 回调时重新解析（规则可能已变化）
			if tc == nil {
				return
			}
			tc.TemplateFolder = path
			ui.tplFolderEntry.SetText(path)
			ui.rebuildTplChecks()
		})
	}()
}

// scanTemplateFiles 扫描文件夹中的模板文件（按文件名自然排序）。
func scanTemplateFiles(dir string) []string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if templateExts[strings.ToLower(filepath.Ext(e.Name()))] {
			files = append(files, e.Name())
		}
	}
	sort.Slice(files, func(i, j int) bool { return naturalLess(files[i], files[j]) })
	return files
}

// naturalLess 按自然顺序比较两个文件名：
// 数字部分按数值大小比较（1、2、10、11），其余部分按字典序比较。
func naturalLess(a, b string) bool {
	ai, bi := 0, 0
	for ai < len(a) && bi < len(b) {
		ca, cb := a[ai], b[bi]
		da, db := ca >= '0' && ca <= '9', cb >= '0' && cb <= '9'
		if da && db {
			ja, jb := ai+1, bi+1
			for ja < len(a) && a[ja] >= '0' && a[ja] <= '9' {
				ja++
			}
			for jb < len(b) && b[jb] >= '0' && b[jb] <= '9' {
				jb++
			}
			// 去掉前导零后比较数值大小
			ta := strings.TrimLeft(a[ai:ja], "0")
			tb := strings.TrimLeft(b[bi:jb], "0")
			if len(ta) != len(tb) {
				return len(ta) < len(tb)
			}
			if ta != tb {
				return ta < tb
			}
			// 数值相同时前导零多者排在前面（如 "01" 在 "1" 前）
			if ja-ai != jb-bi {
				return ja-ai < jb-bi
			}
			ai, bi = ja, jb
			continue
		}
		if ca != cb {
			return ca < cb
		}
		ai++
		bi++
	}
	return len(a)-ai < len(b)-bi
}
