// 执行区（固定在窗口底部）：生成前占位符校验 → 开始生成 → 进度条 + 实时日志 → 取消 / 结果摘要。
//
// 线程模型：core.Run 在独立 goroutine 中执行，通过 RunCallbacks 回传
// 日志与进度；所有 UI 更新经 fyne.Do 切回主线程（fyne.Do 从主线程调用
// 时为异步入队，同样安全）。取消采用 atomic 布尔标志，core.Run 在每个
// 文件开始前检查，完成当前文件后即中止。
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"docgen/internal/core"
)

// pageRun 构建底部执行面板：按钮 + 进度、实时日志（限高滚动）、结果摘要。
func (ui *App) pageRun() fyne.CanvasObject {
	ui.startBtn = widget.NewButtonWithIcon("开始生成", theme.MediaPlayIcon(), ui.startGeneration)
	ui.startBtn.Importance = widget.HighImportance
	ui.cancelBtn = widget.NewButtonWithIcon("取消生成", theme.MediaStopIcon(), ui.cancelGeneration)
	ui.cancelBtn.Disable()
	ui.openFolderBtn = widget.NewButtonWithIcon("打开输出文件夹", theme.FolderOpenIcon(), func() {
		if err := openExplorer(ui.cfg.OutputFolder); err != nil {
			dialog.ShowError(err, ui.win)
		}
	})
	ui.logWinBtn = widget.NewButtonWithIcon("查看执行日志", theme.DocumentIcon(), ui.openLogWindow)

	ui.progressBar = widget.NewProgressBar()
	ui.progressLabel = widget.NewLabel("尚未开始")
	ui.progressLabel.TextStyle = fyne.TextStyle{Bold: true}

	ui.resultLabel = widget.NewLabel("配置完成后，点击「开始生成」批量生成文档。")
	ui.resultLabel.Wrapping = fyne.TextWrapWord
	// 结果可能很长（失败明细），固定高度并内置滚动，防止把底部面板撑得过高
	resultScroll := container.NewVScroll(ui.resultLabel)
	resultScroll.SetMinSize(fyne.NewSize(0, 56))

	// 日志区：VBox（每行一个 Label）+ 垂直滚动，追加后自动滚动到底部。
	// 注意不用 widget.List：List 假定所有条目等高，与自动换行的日志行
	// 冲突，会导致布局计算异常（页面底部内容被挤出可视区）。
	// logLines / logLabels 的并发访问由 logMu 保护。
	ui.logBox = container.NewVBox(widget.NewLabel("生成日志将显示在这里"))
	ui.logScroll = container.NewVScroll(ui.logBox)
	ui.logScroll.SetMinSize(fyne.NewSize(0, 136))

	hint := themedLabel("开始前自动校验模板中的【列名】是否匹配 Excel 标题列，发现问题会提示确认。"+
		"执行日志默认隐藏，点击「查看执行日志」弹出，逐文件显示成功/失败与原因，可随时打开。",
		theme.ColorNamePlaceHolder, false)

	return sectionCard("6 · 执行", vbox(cardGap,
		hint,
		hbox(cardGap, ui.startBtn, ui.cancelBtn, ui.openFolderBtn, ui.logWinBtn),
		container.NewBorder(nil, nil, ui.progressLabel, nil, ui.progressBar),
		ui.logScroll,
		resultScroll,
	))
}

// startGeneration 「开始生成」：快照配置 → 生成前校验（必要时弹窗确认）→ core.Run。
func (ui *App) startGeneration() {
	if ui.running {
		return
	}
	// 配置快照：生成期间用户仍可编辑各页面，快照保证本次生成不受影响
	cfg := cloneConfig(ui.cfg)
	colMap := make(map[string]int, len(ui.colMap))
	for k, v := range ui.colMap {
		colMap[k] = v
	}

	ui.running = true
	ui.cancelFlag.Store(false)
	ui.startBtn.Disable()
	ui.cancelBtn.Enable()
	ui.progressBar.SetValue(0)
	ui.progressLabel.SetText("准备中…")
	ui.resultLabel.SetText("")
	ui.setStatus("正在生成…")
	ui.logMu.Lock()
	ui.logLines = nil
	ui.logLabels = nil
	ui.logLevels = nil
	ui.logWinLabels = nil
	ui.logMu.Unlock()
	ui.logBox.RemoveAll()
	if ui.logWinBox != nil {
		ui.logWinBox.Objects = nil
		ui.logWinBox.Refresh()
	}

	// 执行日志窗口默认隐藏，点击「查看执行日志」时才弹出；
	// 若窗口此前已打开过（仅隐藏），这里只更新标题不显示。
	if ui.logWin != nil {
		ui.logWin.SetTitle("执行日志 — 正在生成…")
	}

	go func() {
		// ---------- 1. 生成前校验 ----------
		warnings, checked := preflightChecks(cfg, colMap)
		if len(warnings) > 0 {
			for _, w := range warnings {
				ui.appendLog("[校验] " + w)
			}
			proceed := make(chan bool, 1)
			fyne.Do(func() {
				// 对话框任何关闭方式（确认/取消/ESC/×）都会触发回调
				dialog.NewCustomConfirm("生成前校验", "仍然生成", "取消生成",
					preflightWidget(warnings),
					func(ok bool) { proceed <- ok },
					ui.win).Show()
			})
			if !<-proceed {
				ui.appendLog("[取消] 用户取消，未开始生成")
				fyne.Do(func() {
					ui.finishRun(core.RunResult{Canceled: true, Summary: "已取消（未开始生成）"}, "")
				})
				return
			}
			ui.appendLog("[校验] 用户确认继续生成")
		} else if checked > 0 {
			ui.appendLog(fmt.Sprintf("[校验] 占位符校验通过（检查了 %d 个模板）", checked))
		}

		// ---------- 2. 执行生成 ----------
		result := core.Run(cfg, &core.RunCallbacks{
			Log:       ui.appendLog,
			Progress:  func(done, total int) { fyne.Do(func() { ui.setProgress(done, total) }) },
			Cancelled: func() bool { return ui.cancelFlag.Load() },
		})
		fyne.Do(func() { ui.finishRun(result, cfg.OutputFolder) })
	}()
}

// cancelGeneration 「取消生成」：置取消标志，core.Run 完成当前文件后中止。
func (ui *App) cancelGeneration() {
	if !ui.running {
		return
	}
	ui.cancelFlag.Store(true)
	ui.cancelBtn.Disable()
	ui.appendLog("[取消] 已请求停止，将在完成当前文件后中止…")
}

// appendLog 追加一行日志并刷新主窗口日志区与执行日志窗口（任意线程可调用）。
func (ui *App) appendLog(line string) {
	ui.logMu.Lock()
	ui.logLines = append(ui.logLines, line)
	ui.logLevels = append(ui.logLevels, classifyLogLevel(line))
	ui.logMu.Unlock()
	fyne.Do(ui.refreshLog)
}

// refreshLog 把新增日志行增量加进日志区并滚动到底部（仅主线程调用）。
func (ui *App) refreshLog() {
	ui.logMu.Lock()
	n := len(ui.logLines)
	for i := len(ui.logLabels); i < n; i++ {
		l := widget.NewLabel(ui.logLines[i])
		l.Wrapping = fyne.TextWrapBreak
		ui.logLabels = append(ui.logLabels, l)
		ui.logBox.Add(l)
	}
	ui.logMu.Unlock()
	if n > 0 {
		ui.logScroll.ScrollToBottom()
	}
	ui.refreshLogWindow()
}

// ---------- 执行日志窗口 ----------

// logLevel 日志行类别（执行日志窗口按类别着色）。
type logLevel int

const (
	logLevelNormal  logLevel = iota // 默认
	logLevelInfo                    // 配置/规则/文件夹等提示
	logLevelWarn                    // 校验/取消等提醒
	logLevelSuccess                 // 成功（生成/完成）
	logLevelFailure                 // 失败
)

// classifyLogLevel 根据日志行前缀判断类别。
func classifyLogLevel(line string) logLevel {
	switch {
	case strings.HasPrefix(line, "[失败]"):
		return logLevelFailure
	case strings.HasPrefix(line, "[生成]"), strings.HasPrefix(line, "[完成]"):
		return logLevelSuccess
	case strings.HasPrefix(line, "[取消]"), strings.HasPrefix(line, "[校验]"):
		return logLevelWarn
	case strings.HasPrefix(line, "[配置]"), strings.HasPrefix(line, "[规则]"),
		strings.HasPrefix(line, "[文件夹]"):
		return logLevelInfo
	default:
		return logLevelNormal
	}
}

// coloredLogLabel 按类别着色的日志行控件。
func coloredLogLabel(text string, lv logLevel) *widget.Label {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapBreak
	switch lv {
	case logLevelSuccess:
		l.Importance = widget.SuccessImportance
	case logLevelFailure:
		l.Importance = widget.DangerImportance
	case logLevelWarn:
		l.Importance = widget.WarningImportance
	case logLevelInfo:
		l.Importance = widget.HighImportance
	}
	return l
}

// openLogWindow 打开（或重新显示并聚焦）执行日志窗口。仅主线程调用。
// 窗口内按行着色实时展示日志：成功绿色、失败红色、提醒黄色、配置信息蓝色。
func (ui *App) openLogWindow() {
	if ui.a == nil {
		ui.setStatus("执行日志窗口不可用")
		return
	}
	if ui.logWin == nil {
		ui.logWinBox = container.NewVBox()
		ui.logWinScroll = container.NewVScroll(ui.logWinBox)
		ui.logWinScroll.SetMinSize(fyne.NewSize(720, 420))
		win := ui.a.NewWindow("执行日志")
		win.Resize(fyne.NewSize(840, 560))
		win.SetContent(ui.logWinScroll)
		// 点 × 只隐藏窗口，后续生成仍可重新弹出
		win.SetCloseIntercept(func() { ui.logWin.Hide() })
		ui.logWin = win
	}
	ui.logWin.Show()
	ui.logWin.RequestFocus()
	ui.refreshLogWindow()
	if ui.logWinScroll != nil {
		ui.logWinScroll.ScrollToBottom()
	}
}

// refreshLogWindow 把新增日志行增量加进执行日志窗口并滚动到底部（仅主线程调用）。
func (ui *App) refreshLogWindow() {
	if ui.logWin == nil || ui.logWinBox == nil {
		return
	}
	ui.logMu.Lock()
	n := len(ui.logLines)
	for i := len(ui.logWinLabels); i < n; i++ {
		l := coloredLogLabel(ui.logLines[i], ui.logLevels[i])
		ui.logWinLabels = append(ui.logWinLabels, l)
		ui.logWinBox.Add(l)
	}
	total := len(ui.logWinLabels)
	ui.logMu.Unlock()
	if total > 0 && ui.logWinScroll != nil {
		ui.logWinScroll.ScrollToBottom()
	}
}

// setProgress 更新进度条与进度文字（仅主线程调用）。
func (ui *App) setProgress(done, total int) {
	if total > 0 {
		ui.progressBar.SetValue(float64(done) / float64(total))
		ui.progressLabel.SetText(fmt.Sprintf("%d / %d", done, total))
	} else {
		ui.progressBar.SetValue(0)
		ui.progressLabel.SetText("0 / 0")
	}
}

// finishRun 收尾：恢复按钮、展示结果摘要、自动打开输出文件夹（仅主线程调用）。
func (ui *App) finishRun(result core.RunResult, outputFolder string) {
	ui.running = false
	ui.cancelFlag.Store(false)
	ui.startBtn.Enable()
	ui.cancelBtn.Disable()
	if result.Canceled {
		ui.progressLabel.SetText("已取消")
	} else if result.Total > 0 {
		ui.setProgress(result.Total, result.Total)
	}
	ui.resultLabel.SetText(formatResult(result))
	if result.Summary != "" {
		ui.setStatus(result.Summary)
	}
	// 执行日志窗口标题带上结果摘要
	if ui.logWin != nil {
		title := "执行日志"
		if result.Summary != "" {
			title = "执行日志 — " + result.Summary
			if len([]rune(title)) > 60 {
				title = string([]rune(title)[:60]) + "…"
			}
		}
		ui.logWin.SetTitle(title)
	}
	// 完成后自动打开输出文件夹（取消或未实际生成时不打开）
	if !result.Canceled && result.Total > 0 {
		_ = openExplorer(outputFolder)
	}
}

// formatResult 把运行结果整理为多行文本（含失败明细，最多列 50 条）。
func formatResult(r core.RunResult) string {
	var b strings.Builder
	if r.Canceled {
		b.WriteString("已手动取消。")
	}
	if r.Summary != "" {
		b.WriteString(r.Summary)
	} else if r.Total == 0 {
		b.WriteString("未生成任何文件，请检查日志中的失败原因。")
	}
	if r.FailedCount > 0 {
		b.WriteString("\n\n失败明细：")
		n := 0
		for _, d := range r.Details {
			if d.Status != "failed" {
				continue
			}
			if n >= 50 {
				fmt.Fprintf(&b, "\n…（其余 %d 条省略）", r.FailedCount-n)
				break
			}
			fmt.Fprintf(&b, "\n· %s — %s", d.Name, d.Note)
			n++
		}
	}
	return b.String()
}

// preflightWidget 构建校验警告对话框内容（可滚动，防止警告过多撑爆对话框）。
func preflightWidget(warnings []string) fyne.CanvasObject {
	lbl := widget.NewLabel("以下问题可能导致占位符无法替换或部分文件生成失败：\n\n" +
		strings.Join(warnings, "\n"))
	lbl.Wrapping = fyne.TextWrapWord
	scroll := container.NewVScroll(lbl)
	scroll.SetMinSize(fyne.NewSize(560, 320))
	return scroll
}

// preflightChecks 生成前校验：模板占位符与 Excel 标题列比对，
// 以及内容规则 / 命名项引用列的存在性检查。
// 返回（警告列表, 实际检查的模板数）。无数据时返回空（参数问题交给 core.Run 报错）。
func preflightChecks(cfg core.RunConfig, colMap map[string]int) ([]string, int) {
	if len(colMap) == 0 {
		return nil, 0
	}
	hasCol := func(name string) bool { _, ok := colMap[name]; return ok }
	var warns []string

	// 1. 内容规则的标题列、命名项引用的列
	for i, r := range cfg.ContentRules {
		if !r.Enabled || strings.TrimSpace(r.TitleColumn) == "" {
			continue
		}
		if !hasCol(r.TitleColumn) {
			warns = append(warns, fmt.Sprintf(
				"内容规则#%d：标题列「%s」在 Excel 标题行中不存在（该规则将匹配不到数据行）",
				i+1, r.TitleColumn))
		}
	}
	for _, item := range cfg.NamingItems {
		if item.Type == "column" && item.Key != "" && !hasCol(item.Key) {
			warns = append(warns, fmt.Sprintf(
				"命名项引用的列「%s」在 Excel 标题行中不存在（文件命名会失败）", item.Key))
		}
	}

	// 2. 收集启用规则引用的模板（跨规则去重）
	type tplRef struct{ folder, name string }
	seenTpl := map[tplRef]bool{}
	var tpls []tplRef
	collect := func(tc core.TemplateConfig) {
		names, terr := core.ResolveTemplates(tc, cfg.Groups, cfg.DefaultTemplateDir)
		if terr != "" {
			return // 模板缺失等问题由 core.Run 记录，这里不重复报
		}
		folder := strings.TrimSpace(tc.TemplateFolder)
		if folder == "" {
			folder = cfg.DefaultTemplateDir
		}
		for _, n := range names {
			ref := tplRef{folder, n}
			if !seenTpl[ref] {
				seenTpl[ref] = true
				tpls = append(tpls, ref)
			}
		}
	}
	for _, r := range cfg.ContentRules {
		if r.Enabled {
			collect(r.TemplateConfig)
		}
	}
	for _, r := range cfg.NumberRules {
		if r.Enabled {
			collect(r.TemplateConfig)
		}
	}

	// 3. 逐模板提取占位符并比对标题列
	for _, t := range tpls {
		names, err := core.TemplatePlaceholders(filepath.Join(t.folder, t.name))
		if err != nil {
			warns = append(warns, fmt.Sprintf("模板「%s」无法读取：%v", t.name, err))
			continue
		}
		var missing []string
		for _, n := range names {
			if !hasCol(n) {
				missing = append(missing, "【"+n+"】")
			}
		}
		if len(missing) > 0 {
			warns = append(warns, fmt.Sprintf(
				"模板「%s」中的占位符 %s 在 Excel 标题行中找不到对应列（将原样保留）",
				t.name, strings.Join(missing, "、")))
		}
	}
	return warns, len(tpls)
}

// cloneConfig 深拷贝配置快照（切片与映射逐层复制），
// 避免生成期间用户在页面上编辑配置影响正在进行的生成。
func cloneConfig(c core.RunConfig) core.RunConfig {
	c2 := c
	c2.ContentRules = make([]core.ContentRule, len(c.ContentRules))
	for i, r := range c.ContentRules {
		r.SelectedGroups = append([]string(nil), r.SelectedGroups...)
		r.SelectedTemplates = append([]string(nil), r.SelectedTemplates...)
		c2.ContentRules[i] = r
	}
	c2.NumberRules = make([]core.NumberRule, len(c.NumberRules))
	for i, r := range c.NumberRules {
		r.SelectedGroups = append([]string(nil), r.SelectedGroups...)
		r.SelectedTemplates = append([]string(nil), r.SelectedTemplates...)
		c2.NumberRules[i] = r
	}
	c2.NamingItems = append([]core.NamingItem(nil), c.NamingItems...)
	c2.Groups = make(map[string][]string, len(c.Groups))
	for k, v := range c.Groups {
		c2.Groups[k] = append([]string(nil), v...)
	}
	return c2
}

// openExplorer 在资源管理器中打开文件夹。
func openExplorer(folder string) error {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return fmt.Errorf("输出文件夹未设置")
	}
	if st, err := os.Stat(folder); err != nil || !st.IsDir() {
		return fmt.Errorf("输出文件夹不存在：%s", folder)
	}
	return exec.Command("explorer", folder).Start()
}
