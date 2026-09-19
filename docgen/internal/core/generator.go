package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ---------- 配置结构（GUI 与 CLI 共用，可序列化为 JSON） ----------

// TemplateConfig 每条规则共用的模板选择配置。
type TemplateConfig struct {
	TemplateFolder    string   `json:"template_folder"`                // 模板所在文件夹
	TemplateMode      string   `json:"template_mode"`                   // "all"（按勾选）| "group"（按分组）
	SelectedGroups    []string `json:"selected_groups"`                 // mode=group 时选用的分组名
	SelectedTemplates []string `json:"selected_templates"`              // mode=all 时勾选的模板文件名
}

// ContentRule 内容模式规则：选中「标题列下内容 = 指定值」的所有数据行。
type ContentRule struct {
	Enabled      bool   `json:"enabled"`        // 是否启用（新增的 GUI 功能）
	TitleColumn  string `json:"title_column"`   // 标题列名
	ContentValue string `json:"content_value"`  // 该列下要匹配的内容值
	TemplateConfig
}

// NumberRule 数字模式规则：选中「第 x 行至第 y 行」范围内的数据行。
type NumberRule struct {
	Enabled  bool `json:"enabled"`
	RowStart int  `json:"row_start"`
	RowEnd   int  `json:"row_end"`
	TemplateConfig
}

// NamingItem 文件命名项：按顺序叠加。
type NamingItem struct {
	Type string `json:"type"` // "column"（标题列值）| "template"（模板显示名）
	Key  string `json:"key"`  // type=column 时为列名
}

// RunConfig 一次批量生成的完整配置。
type RunConfig struct {
	ExcelPath         string              `json:"excel_path"`          // 数据源 Excel 文件
	SheetName         string              `json:"sheet_name"`          // 数据 Sheet 名
	HeaderRow         int                 `json:"header_row"`          // 标题所在行（1 基）
	OutputFolder      string              `json:"output_folder"`       // 生成文件存储位置
	CreateSubfolder   bool                `json:"create_subfolder"`    // 是否按命名项建子文件夹
	ContentRules      []ContentRule       `json:"content_rules"`
	NumberRules       []NumberRule        `json:"number_rules"`
	NamingItems       []NamingItem        `json:"naming_items"`
	Groups            map[string][]string `json:"groups"`              // 模板分组：分组名 → 模板文件名列表
	DefaultTemplateDir string             `json:"default_template_dir"` // 规则未填模板文件夹时的默认目录
}

// RunCallbacks 生成过程中的回调（GUI 用于实时日志、进度条、取消按钮）。
type RunCallbacks struct {
	Log       func(line string)     // 实时日志行
	Progress  func(done, total int) // 进度（total 在规划完成后固定）
	Cancelled func() bool           // 返回 true 时在下一个文件前中止
}

// DetailEntry 单个文件的生成结果明细。
type DetailEntry struct {
	Status string `json:"status"` // "success" | "failed"
	Name   string `json:"name"`   // 生成文件相对路径或模板名
	Note   string `json:"note"`   // 说明（行号、原因等）
}

// RunResult 一次批量生成的汇总结果。
type RunResult struct {
	Success      bool          `json:"success"`
	Total        int           `json:"total"`
	SuccessCount int           `json:"success_count"`
	FailedCount  int           `json:"failed_count"`
	Details      []DetailEntry `json:"details"`
	Summary      string        `json:"summary"`
	Canceled     bool          `json:"canceled"`
}

// genTask 一个待生成的文件（数据行 × 模板）。
type genTask struct {
	ruleLabel string // 规则标签，如 "内容规则#1"
	rowIdx    int    // 数据行索引（0 基，对应 Excel 中第 rowIdx+1 行）
	tplFolder string
	tplName   string
}

// Run 执行一次批量生成。对应 Python 版 run(params)。
// 校验失败时返回 Success=false 且 Summary 为失败原因。
func Run(cfg RunConfig, cb *RunCallbacks) RunResult {
	var logf = func(format string, args ...interface{}) {}
	var progress = func(done, total int) {}
	var cancelled = func() bool { return false }
	if cb != nil {
		if cb.Log != nil {
			logf = func(format string, args ...interface{}) { cb.Log(fmt.Sprintf(format, args...)) }
		}
		if cb.Progress != nil {
			progress = cb.Progress
		}
		if cb.Cancelled != nil {
			cancelled = cb.Cancelled
		}
	}
	fail := func(msg string) RunResult {
		logf("[失败] %s", msg)
		return RunResult{Success: false, Summary: msg}
	}

	logf("========================================")
	logf("文档批量生成")
	logf("========================================")

	// ---------- 1. 参数校验 ----------
	if strings.TrimSpace(cfg.ExcelPath) == "" {
		return fail("请选择有效的本地 Excel 数据表格")
	}
	if _, err := os.Stat(cfg.ExcelPath); err != nil {
		return fail("请选择有效的本地 Excel 数据表格")
	}
	if cfg.HeaderRow < 1 {
		return fail("标题所在行必须大于 0")
	}
	outputFolder := strings.TrimSpace(cfg.OutputFolder)
	if outputFolder == "" {
		return fail("请选择生成文件存储位置")
	}
	if st, err := os.Stat(outputFolder); err != nil || !st.IsDir() {
		return fail("存储位置不存在或不是目录：" + outputFolder)
	}
	if strings.TrimSpace(cfg.SheetName) == "" {
		return fail("请在配置中选择数据 Sheet")
	}
	if len(cfg.ContentRules)+len(cfg.NumberRules) == 0 {
		return fail("请至少新增一条数据行规则")
	}
	enabledRules := 0
	for _, r := range cfg.ContentRules {
		if r.Enabled {
			enabledRules++
		}
	}
	for _, r := range cfg.NumberRules {
		if r.Enabled {
			enabledRules++
		}
	}
	if enabledRules == 0 {
		return fail("没有已启用的数据行规则")
	}
	if len(cfg.NamingItems) == 0 {
		return fail("请至少选择一个命名选项")
	}
	if cfg.Groups == nil {
		cfg.Groups = map[string][]string{}
	}

	logf("[配置] 数据源：%s | 标题行：第 %d 行 | Sheet：%s",
		filepath.Base(cfg.ExcelPath), cfg.HeaderRow, cfg.SheetName)
	logf("[配置] 内容规则 %d 条 | 数字规则 %d 条 | 命名项 %d 个",
		len(cfg.ContentRules), len(cfg.NumberRules), len(cfg.NamingItems))
	if cfg.CreateSubfolder {
		logf("[配置] 是否新建文件夹：是（按命名项建立子文件夹）")
	} else {
		logf("[配置] 是否新建文件夹：否（直接生成至存放位置）")
	}

	// ---------- 2. 加载数据 ----------
	rows, err := LoadSheetRows(cfg.ExcelPath, cfg.SheetName)
	if err != nil {
		return fail(err.Error())
	}
	rows = TruncateTrailingEmptyRows(rows)
	if len(rows) == 0 {
		return fail("Sheet「" + cfg.SheetName + "」无数据")
	}
	colMap, headerNames := BuildHeaderMap(rows, cfg.HeaderRow)
	if len(colMap) == 0 {
		return fail("标题行第 " + strconv.Itoa(cfg.HeaderRow) + " 行为空或全部为空单元格")
	}
	{
		show := headerNames
		suffix := ""
		if len(show) > 8 {
			show = show[:8]
			suffix = "..."
		}
		logf("[配置] 识别到 %d 个列标题：%s%s", len(headerNames), strings.Join(show, "、"), suffix)
	}

	// ---------- 3. 规划：逐规则选行、解析模板，展开为任务列表 ----------
	var details []DetailEntry
	var tasks []genTask

	planContentRule := func(i int, rule ContentRule) {
		label := "内容规则#" + strconv.Itoa(i+1)
		if !rule.Enabled {
			logf("[规则][%s] 已跳过（未启用）", label)
			return
		}
		tc := strings.TrimSpace(rule.TitleColumn)
		cv := strings.TrimSpace(rule.ContentValue)
		if tc == "" {
			details = append(details, DetailEntry{"failed", label, "未选择标题列"})
			logf("[规则][%s] 未选择标题列，跳过", label)
			return
		}
		if cv == "" {
			details = append(details, DetailEntry{"failed", label, "未选择内容值"})
			logf("[规则][%s] 未选择内容值，跳过", label)
			return
		}
		rowIdxs := SelectContentRows(rows, cfg.HeaderRow, colMap, tc, cv)
		logf("[规则][%s] 内容模式「%s=%s」 → %d 行数据", label, tc, cv, len(rowIdxs))
		if len(rowIdxs) == 0 {
			details = append(details, DetailEntry{"failed", label, "未匹配到任何数据行"})
			logf("[规则][%s] 未匹配到数据行", label)
			return
		}
		appendTasks(&tasks, &details, logf, label, rule.TemplateConfig, cfg, rowIdxs)
	}

	planNumberRule := func(i int, rule NumberRule) {
		label := "数字规则#" + strconv.Itoa(i+1)
		if !rule.Enabled {
			logf("[规则][%s] 已跳过（未启用）", label)
			return
		}
		if rule.RowStart < 1 || rule.RowEnd < rule.RowStart {
			details = append(details, DetailEntry{"failed", label,
				"行号范围无效：" + strconv.Itoa(rule.RowStart) + "-" + strconv.Itoa(rule.RowEnd)})
			logf("[规则][%s] 行号范围无效，跳过", label)
			return
		}
		rowIdxs := SelectNumberRows(rows, rule.RowStart, rule.RowEnd)
		logf("[规则][%s] 数字模式 第 %d-%d 行 → %d 行数据", label, rule.RowStart, rule.RowEnd, len(rowIdxs))
		if len(rowIdxs) == 0 {
			details = append(details, DetailEntry{"failed", label, "未匹配到任何数据行"})
			logf("[规则][%s] 未匹配到数据行", label)
			return
		}
		appendTasks(&tasks, &details, logf, label, rule.TemplateConfig, cfg, rowIdxs)
	}

	// 内容规则先于数字规则执行（均按列表顺序），与 Python 版一致
	for i, rule := range cfg.ContentRules {
		planContentRule(i, rule)
	}
	for i, rule := range cfg.NumberRules {
		planNumberRule(i, rule)
	}

	// ---------- 4. 执行 ----------
	total := len(tasks)
	successCount, failedCount := 0, 0
	usedPaths := map[string]bool{}
	createdSubfolders := map[string]bool{}
	canceled := false
	progress(0, total)

	for ti, task := range tasks {
		if cancelled() {
			canceled = true
			logf("[取消] 已停止生成（完成 %d/%d）", ti, total)
			break
		}
		fields := RowToFields(rows[task.rowIdx], colMap)

		// 确定目标文件夹（按是否新建文件夹）
		targetFolder := outputFolder
		if cfg.CreateSubfolder {
			folderName, ferr := BuildFolderName(cfg.NamingItems, fields)
			if ferr != "" {
				failedCount++
				details = append(details, DetailEntry{"failed", task.tplName,
					"第 " + strconv.Itoa(task.rowIdx+1) + " 行：" + ferr})
				logf("[失败] %s（第 %d 行：%s）", task.tplName, task.rowIdx+1, ferr)
				continue
			}
			targetFolder = filepath.Join(outputFolder, folderName)
			if err := os.MkdirAll(targetFolder, 0o755); err != nil {
				failedCount++
				details = append(details, DetailEntry{"failed", task.tplName,
					"第 " + strconv.Itoa(task.rowIdx+1) + " 行：创建文件夹失败：" + err.Error()})
				logf("[失败] %s（第 %d 行：创建文件夹失败：%v）", task.tplName, task.rowIdx+1, err)
				continue
			}
			if !createdSubfolders[targetFolder] {
				createdSubfolders[targetFolder] = true
				logf("[文件夹] 新建：%s", folderName)
			}
		}

		tplPath := filepath.Join(task.tplFolder, task.tplName)
		ext := filepath.Ext(task.tplName)
		baseName, nerr := BuildFilename(cfg.NamingItems, fields, task.tplName)
		if nerr != "" {
			failedCount++
			details = append(details, DetailEntry{"failed", task.tplName,
				"第 " + strconv.Itoa(task.rowIdx+1) + " 行：" + nerr})
			logf("[失败] %s（第 %d 行：%s）", task.tplName, task.rowIdx+1, nerr)
			continue
		}
		outPath := ResolveOutputPath(targetFolder, baseName, ext, usedPaths)
		ok, msg := generateFile(tplPath, outPath, fields)
		if ok {
			successCount++
			rel, err := filepath.Rel(outputFolder, outPath)
			if err != nil {
				rel = outPath
			}
			details = append(details, DetailEntry{"success", rel,
				"第 " + strconv.Itoa(task.rowIdx+1) + " 行 · " + msg})
			logf("[生成] %s", rel)
		} else {
			failedCount++
			details = append(details, DetailEntry{"failed", task.tplName,
				"第 " + strconv.Itoa(task.rowIdx+1) + " 行：" + msg})
			logf("[失败] %s（第 %d 行：%s）", task.tplName, task.rowIdx+1, msg)
		}
		progress(ti+1, total)
	}

	// ---------- 5. 汇总 ----------
	subfolderInfo := ""
	if cfg.CreateSubfolder && len(createdSubfolders) > 0 {
		subfolderInfo = "，新建文件夹 " + strconv.Itoa(len(createdSubfolders)) + " 个"
	}
	summary := fmt.Sprintf("生成完成：共 %d 个文件，成功 %d，失败 %d%s（输出：%s）",
		total, successCount, failedCount, subfolderInfo, outputFolder)
	if canceled {
		summary = "已手动取消。" + summary
	}
	logf("[完成] %s", summary)

	return RunResult{
		Success:      failedCount == 0 && successCount > 0 && !canceled,
		Total:        total,
		SuccessCount: successCount,
		FailedCount:  failedCount,
		Details:      details,
		Summary:      summary,
		Canceled:     canceled,
	}
}

// appendTasks 解析规则模板并把「数据行 × 模板」展开进任务列表。
func appendTasks(tasks *[]genTask, details *[]DetailEntry, logf func(string, ...interface{}),
	label string, tc TemplateConfig, cfg RunConfig, rowIdxs []int) {
	tplNames, terr := ResolveTemplates(tc, cfg.Groups, cfg.DefaultTemplateDir)
	if terr != "" {
		// 模板缺失视为整条规则失败
		*details = append(*details, DetailEntry{"failed", label, terr})
		logf("[规则][%s] %s", label, terr)
		return
	}
	folder := strings.TrimSpace(tc.TemplateFolder)
	if folder == "" {
		folder = cfg.DefaultTemplateDir
	}
	show := tplNames
	suffix := ""
	if len(show) > 5 {
		show = show[:5]
		suffix = "..."
	}
	logf("[规则][%s] 选用 %d 个模板：%s%s", label, len(tplNames), strings.Join(show, "、"), suffix)
	for _, ridx := range rowIdxs {
		for _, name := range tplNames {
			*tasks = append(*tasks, genTask{label, ridx, folder, name})
		}
	}
}

// SaveConfig 将配置序列化为 JSON 写入文件（GUI 的「记忆上次配置」用）。
func SaveConfig(path string, cfg RunConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadConfig 从 JSON 文件读取配置。文件不存在时返回错误由调用方决定是否忽略。
func LoadConfig(path string) (RunConfig, error) {
	var cfg RunConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
