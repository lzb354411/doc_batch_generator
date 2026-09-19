// Package ui —— Fyne 图形界面。
//
// 布局：单窗口。内容为可滚动单页（1 数据源 → 2 数据行规则 → 3 模板配置
// → 4 文件命名 → 5 输出设置 → 执行面板，滑到最底部即出现「开始执行」），
// 底部仅保留一行状态栏（状态文字 + 保存配置）。无需任何页面切换。
//
// 核心约定：core.RunConfig 是唯一数据源。所有控件的改动即时写回 cfg，
// 点击底部「保存配置」或关闭窗口时持久化到 EXE 同目录的 config.json，
// 启动时自动恢复上次的配置（「记忆上次配置」）。
package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"docgen/internal/core"
)

// App 持有 GUI 全局状态。
type App struct {
	a      fyne.App
	win    fyne.Window
	status *widget.Label

	// suppress > 0 时控件回调不写回配置（批量重建控件、恢复状态时使用），
	// 避免程序赋值触发 OnChanged 造成重复加载或误清数据。
	suppress int

	configPath string         // config.json 路径（EXE 同目录）
	cfg        core.RunConfig // 当前配置（所有页面的唯一数据源）

	// ---------- 数据源缓存（数据源页维护，各页读取） ----------
	rows        [][]core.CellValue // 截断尾部空行后的全部行
	headerNames []string           // 标题列名（按列顺序）
	colMap      map[string]int     // 列名 → 列索引

	// 数据源页控件
	excelEntry     *widget.Entry
	sheetSelect    *widget.Select
	headerRowEntry *widget.Entry
	previewTable   *widget.Table
	previewLabel   *widget.Label

	// 规则页控件
	rulesBox *fyne.Container // 内容规则 + 数字规则的动态行容器

	// 模板页控件
	tplRuleSelect  *widget.Select
	tplRuleKeys    []ruleKey // 与 tplRuleSelect.Options 一一对应
	tplSelKind     int       // 当前选中的规则类别（kindContent/kindNumber，-1 未选）
	tplSelIdx      int       // 当前选中的规则下标
	tplFolderEntry *widget.Entry
	tplModeRadio   *widget.RadioGroup // 模板选择模式：按勾选模板 / 按模板分组
	tplChecksBox   *fyne.Container
	tplChecks      []*widget.Check // 与 tplFiles 一一对应
	tplFiles       []string        // 模板文件夹扫描结果（按文件名排序）

	// 命名页控件
	namingBox     *fyne.Container
	namingPreview *widget.Label

	// 输出页控件
	outputEntry    *widget.Entry
	subfolderCheck *widget.Check

	// 配置区滚动容器（调试定位用）
	vscroll *container.Scroll

	// 执行页控件与运行状态
	startBtn      *widget.Button
	cancelBtn     *widget.Button
	openFolderBtn *widget.Button
	progressBar   *widget.ProgressBar
	progressLabel *widget.Label
	logScroll     *container.Scroll // 日志区滚动容器
	logBox        *fyne.Container   // 日志行容器（VBox，每行一个 Label，支持变高）
	logLabels     []*widget.Label   // 已创建的日志行控件（与 logLines 前 n 个一一对应）
	logLines      []string          // 实时日志行（并发访问由 logMu 保护）
	logLevels     []logLevel        // 每行日志的类别（并发访问由 logMu 保护）
	logMu         sync.Mutex
	resultLabel   *widget.Label
	running       bool        // 是否正在生成（仅主线程读写）
	cancelFlag    atomic.Bool // 取消标志（生成 goroutine 与 UI 线程共享）

	// 执行日志窗口（独立窗口：着色显示成功/失败/原因）
	logWinBtn    *widget.Button  // 打开执行日志窗口的按钮
	logWin       fyne.Window     // 执行日志窗口（nil 表示尚未创建）
	logWinBox    *fyne.Container // 日志窗口内容（VBox，每行一个 Label）
	logWinScroll *container.Scroll
	logWinLabels []*widget.Label // 日志窗口已创建的控件（与 logLines 前 n 个一一对应）
}

// Version 应用版本号。正式构建时通过 -ldflags "-X docgen/internal/ui.Version=1.0.0" 注入。
var Version = "dev"

// Run 启动 GUI 主窗口（阻塞直至窗口关闭）。
func Run() error {
	a := app.NewWithID("com.docgen.tool")
	applyTraeTheme(a)

	w := a.NewWindow(fmt.Sprintf("资料文档生成 v%s", Version))
	w.Resize(fyne.NewSize(1080, 720))

	ui := &App{win: w, configPath: resolveConfigPath()}
	ui.a = a
	ui.cfg = defaultConfig()
	if loaded, err := core.LoadConfig(ui.configPath); err == nil {
		ui.cfg = loaded
		if ui.cfg.HeaderRow < 1 {
			ui.cfg.HeaderRow = 1
		}
	}

	ui.build()
	ui.restoreFromConfig()

	// 恢复到顶部：确保「1 数据源」区块（Excel 选择、路径等）一开始就可见，
	// 避免滚动位置残留导致首屏停在中间、看起来像第 1 部分空白。
	ui.vscroll.ScrollToTop()

	// 调试钩子：DOCGEN_START_PAGE 指定启动后直接切到的页面下标（0-5）
	if p := os.Getenv("DOCGEN_START_PAGE"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			ui.selectPage(n)
		}
	}

	// 调试钩子：DOCGEN_AUTOSTART=1 时窗口显示后自动开始生成（自动化冒烟测试用）
	if os.Getenv("DOCGEN_AUTOSTART") == "1" {
		go func() {
			time.Sleep(1500 * time.Millisecond)
			fyne.Do(func() { ui.startGeneration() })
		}()
	}

	// 调试钩子：DOCGEN_DEBUG=1 时在执行页日志输出画布尺寸与缩放（DPI 排查用）
	if os.Getenv("DOCGEN_DEBUG") == "1" {
		go func() {
			time.Sleep(3000 * time.Millisecond)
			c := ui.win.Canvas()
			fyne.Do(func() {
				off := ui.vscroll.Offset.Y
				ui.appendLog(fmt.Sprintf("[调试] canvas=%v scale=%.2f vscroll.Offset.Y=%.0f",
					c.Size(), c.Scale(), off))
				ui.logMu.Lock()
				dump := fmt.Sprintf("size=%v scale=%.2f offsetY=%.2f logWin=%v logLines=%d logWinLabels=%d\n",
					c.Size(), c.Scale(), off, ui.logWin != nil, len(ui.logLines), len(ui.logWinLabels))
				for _, l := range ui.logLines {
					dump += l + "\n"
				}
				ui.logMu.Unlock()
				os.WriteFile(os.TempDir()+"\\docgen_debug.txt", []byte(dump), 0644)
			})
		}()
	}

	// 用户点窗口「×」时先静默保存配置再关闭（记忆上次配置）
	w.SetCloseIntercept(func() {
		ui.saveConfigQuiet()
		w.Close()
	})

	w.ShowAndRun()
	return nil
}

// defaultConfig 返回带默认值的新配置。
func defaultConfig() core.RunConfig {
	return core.RunConfig{
		HeaderRow: 1,
		Groups:    map[string][]string{},
	}
}

// resolveConfigPath 确定 config.json 位置：
// 环境变量 DOCGEN_CONFIG 优先（开发调试用），其次是 EXE 同目录；
// go run 的临时构建目录不放配置，退回当前工作目录。
func resolveConfigPath() string {
	if p := os.Getenv("DOCGEN_CONFIG"); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if !strings.Contains(dir, "go-build") {
			return filepath.Join(dir, "config.json")
		}
	}
	return "config.json"
}

// build 组装单页滚动布局：6 张白色区块卡片（数据源 → 规则 → 模板 → 命名
// → 输出 → 执行面板）排列在浅灰画布上，底部固定一条白色状态栏（状态文字
// + 保存配置按钮）。
func (ui *App) build() {
	ui.status = widget.NewLabel("就绪")

	config := container.New(vPadLayout{pad: 12, gap: 12},
		ui.pageDataSource(),
		ui.pageRules(),
		ui.pageTemplates(),
		ui.pageNaming(),
		ui.pageOutput(),
		ui.pageRun(),
	)
	vscroll := container.NewVScroll(config)
	ui.vscroll = vscroll

	saveBtn := widget.NewButton("保存配置", func() { ui.saveConfigInteractive() })
	statusBar := footerBar(container.NewBorder(nil, nil, nil, saveBtn, ui.status))

	root := container.NewBorder(nil, statusBar, nil, nil, vscroll)
	ui.win.SetContent(root)
}

// selectPage 单页布局下不再切换页面，保留此入口仅为兼容调试钩子
// （DOCGEN_START_PAGE）：按步骤下标刷新对应区块的动态内容。
func (ui *App) selectPage(i int) {
	switch i {
	case 1:
		ui.rebuildRulesList()
	case 2:
		ui.refreshTemplatesPage()
	case 3:
		ui.rebuildNamingList()
	}
}

// ---------- 配置持久化 ----------

// saveConfigQuiet 保存配置，出错时不打扰用户（关窗时使用）。
func (ui *App) saveConfigQuiet() {
	_ = core.SaveConfig(ui.configPath, ui.cfg)
}

// saveConfigInteractive 保存配置并向用户反馈结果。
func (ui *App) saveConfigInteractive() {
	if err := core.SaveConfig(ui.configPath, ui.cfg); err != nil {
		dialog.ShowError(err, ui.win)
		ui.setStatus("保存配置失败：" + err.Error())
		return
	}
	ui.setStatus("配置已保存：" + ui.configPath)
}

// setStatus 更新底部状态栏文字。
func (ui *App) setStatus(s string) {
	ui.status.SetText(s)
}

// restoreFromConfig 启动时把已保存的配置恢复到各页面控件。
func (ui *App) restoreFromConfig() {
	ui.suppress++
	defer func() { ui.suppress-- }()

	if ui.cfg.ExcelPath != "" {
		ui.excelEntry.SetText(ui.cfg.ExcelPath)
		if _, err := os.Stat(ui.cfg.ExcelPath); err == nil {
			ui.reloadExcel() // 内部完成 Sheet 选择、数据加载与各页刷新
		} else {
			ui.setStatus("上次的 Excel 文件不存在，请重新选择：" + ui.cfg.ExcelPath)
			ui.refreshAllAfterDataChange()
		}
	} else {
		ui.refreshAllAfterDataChange()
	}
	ui.refreshTemplatesPage()
}

// refreshAllAfterDataChange 数据或标题行变化后：重建标题映射并刷新依赖页面。
func (ui *App) refreshAllAfterDataChange() {
	if ui.rows == nil {
		ui.clearData()
		return
	}
	ui.colMap, ui.headerNames = core.BuildHeaderMap(ui.rows, ui.cfg.HeaderRow)
	ui.refreshPreview()
	ui.rebuildRulesList()
	ui.rebuildNamingList()
	ui.updateNamingPreview()
}

// clearData 清空已加载的数据缓存并刷新各页为空状态。
func (ui *App) clearData() {
	ui.rows = nil
	ui.colMap = nil
	ui.headerNames = nil
	ui.refreshPreview()
	ui.rebuildRulesList()
	ui.rebuildNamingList()
	ui.updateNamingPreview()
}

// ---------- 通用小工具 ----------

// containsStr 判断字符串切片是否包含指定值。
func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// setSelect 仅当值存在于选项中时设置下拉框选中项，
// 值不存在或为空时保持未选中（避免 SetSelected 对不存在值的副作用）。
func setSelect(sel *widget.Select, val string) {
	if val == "" || !containsStr(sel.Options, val) {
		return
	}
	sel.SetSelected(val)
}

// withMinHeight 把 obj 包装成「占满可用宽度、高度至少为 min」的容器。
// 用于单页滚动布局中给预览表格等限高，避免其占用过多纵向空间，
// 宽度随窗口尺寸自适应（不会出现横向重叠/遮挡）。
func withMinHeight(obj fyne.CanvasObject, min float32) fyne.CanvasObject {
	return container.New(minHeightLayout{min: min}, obj)
}

// minHeightLayout 让子对象扩满容器尺寸，但容器最小高度固定为 min。
type minHeightLayout struct{ min float32 }

func (l minHeightLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		if o.Visible() {
			o.Resize(size)
		}
	}
}

func (l minHeightLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, l.min)
}
