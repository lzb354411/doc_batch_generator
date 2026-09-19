package core

// 主流程测试：Run（成功 / 各类校验失败 / 取消 / 规则未匹配）、
// SaveConfig / LoadConfig 往返。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testRunConfig 构造一次完整的有效配置（内容规则：类型=开工，
// 用 docx 模板，按项目名称建子文件夹）。
func testRunConfig(outDir string) RunConfig {
	return RunConfig{
		ExcelPath:       testDataXLSX,
		SheetName:       "Sheet1",
		HeaderRow:       3,
		OutputFolder:    outDir,
		CreateSubfolder: true,
		ContentRules: []ContentRule{{
			Enabled:      true,
			TitleColumn:  "类型",
			ContentValue: "开工",
			TemplateConfig: TemplateConfig{
				TemplateFolder:    testTplDir,
				TemplateMode:      "all",
				SelectedTemplates: []string{"1.模板-开工报告.docx"},
			},
		}},
		NamingItems: []NamingItem{{Type: "column", Key: "项目名称"}, {Type: "template"}},
	}
}

func TestRunSuccess(t *testing.T) {
	requireFile(t, testDataXLSX)
	requireFile(t, testTplDir)
	out := t.TempDir()
	cfg := testRunConfig(out)

	var logs []string
	res := Run(cfg, &RunCallbacks{Log: func(line string) { logs = append(logs, line) }})

	if !res.Success {
		t.Fatalf("期望生成成功，实际失败：%s", res.Summary)
	}
	if res.Total != 3 || res.SuccessCount != 3 || res.FailedCount != 0 {
		t.Fatalf("Total=%d Success=%d Failed=%d，期望 3/3/0",
			res.Total, res.SuccessCount, res.FailedCount)
	}
	if res.Canceled {
		t.Error("不应标记为取消")
	}
	// 类型=开工 的数据行：项目A、项目B、项目C（各 1 行 × 1 模板）
	for _, name := range []string{"项目A", "项目B", "项目C"} {
		if _, err := os.Stat(filepath.Join(out, name, name+"开工报告.docx")); err != nil {
			t.Errorf("缺少输出文件：%v", err)
		}
	}
	// 项目D（验收）不应被生成
	if _, err := os.Stat(filepath.Join(out, "项目D")); !os.IsNotExist(err) {
		t.Error("项目D 不应被生成")
	}
	// 日志应当非空
	if len(logs) == 0 {
		t.Error("回调日志为空")
	}
}

func TestRunProgressAndDetails(t *testing.T) {
	requireFile(t, testDataXLSX)
	cfg := testRunConfig(t.TempDir())
	seen := []int{}
	res := Run(cfg, &RunCallbacks{Progress: func(done, total int) {
		seen = append(seen, done)
	}})
	if !res.Success || len(seen) < 1 {
		t.Fatalf("seen=%v res.Success=%v", seen, res.Success)
	}
	if want := 3; seen[len(seen)-1] != want {
		t.Fatalf("最后一次进度 = %d，期望 %d", seen[len(seen)-1], want)
	}
	if len(res.Details) != 3 {
		t.Fatalf("明细数 = %d，期望 3", len(res.Details))
	}
	for _, d := range res.Details {
		if d.Status != "success" {
			t.Fatalf("明细应为 success：%+v", d)
		}
	}
}

func TestRunValidationFailures(t *testing.T) {
	requireFile(t, testDataXLSX)
	out := t.TempDir()
	base := testRunConfig(out)

	cases := []struct {
		name string
		mut  func(*RunConfig)
		want string
	}{
		{"空 Excel 路径", func(c *RunConfig) { c.ExcelPath = "" }, "请选择有效的本地 Excel 数据表格"},
		{"Excel 不存在", func(c *RunConfig) { c.ExcelPath = "不存在.xlsx" }, "请选择有效的本地 Excel 数据表格"},
		{"标题行小于 1", func(c *RunConfig) { c.HeaderRow = 0 }, "标题所在行必须大于 0"},
		{"未选输出目录", func(c *RunConfig) { c.OutputFolder = "" }, "请选择生成文件存储位置"},
		{"输出目录不存在", func(c *RunConfig) { c.OutputFolder = "不存在的目录" }, "存储位置不存在或不是目录"},
		{"未选 Sheet", func(c *RunConfig) { c.SheetName = "" }, "请在配置中选择数据 Sheet"},
		{"没有任何规则", func(c *RunConfig) {
			c.ContentRules = nil
			c.NumberRules = nil
		}, "请至少新增一条数据行规则"},
		{"没有已启用规则", func(c *RunConfig) {
			c.ContentRules = []ContentRule{{Enabled: false, TitleColumn: "类型",
				ContentValue: "开工", TemplateConfig: TemplateConfig{
					TemplateFolder: testTplDir, TemplateMode: "all",
					SelectedTemplates: []string{"1.模板-开工报告.docx"}}}}
		}, "没有已启用的数据行规则"},
		{"没有任何命名项", func(c *RunConfig) { c.NamingItems = nil }, "请至少选择一个命名选项"},
		{"标题行超出数据范围", func(c *RunConfig) { c.HeaderRow = 999 }, "标题行第 999 行为空或全部为空单元格"},
	}
	for _, c := range cases {
		cfg := base
		c.mut(&cfg)
		res := Run(cfg, nil)
		if res.Success {
			t.Errorf("%s：本应失败，结果成功：%s", c.name, res.Summary)
			continue
		}
		if res.Summary == "" {
			t.Errorf("%s：失败但没有汇总信息", c.name)
		}
		if !strings.Contains(res.Summary, c.want) {
			t.Errorf("%s：汇总 %q 应包含 %q", c.name, res.Summary, c.want)
		}
	}
}

func TestRunCancel(t *testing.T) {
	requireFile(t, testDataXLSX)
	cfg := testRunConfig(t.TempDir())

	counter := 0
	res := Run(cfg, &RunCallbacks{
		Cancelled: func() bool { counter++; return counter > 2 },
	})
	if !res.Canceled {
		t.Fatal("应标记为已取消")
	}
	if res.Success {
		t.Fatal("取消后不应成功")
	}
	if res.SuccessCount != 2 {
		t.Fatalf("取消前应完成 2 个，实际 %d", res.SuccessCount)
	}
	if !strings.Contains(res.Summary, "已手动取消") {
		t.Fatalf("汇总应包含取消提示：%s", res.Summary)
	}
}

func TestRunRuleMatchNone(t *testing.T) {
	requireFile(t, testDataXLSX)
	cfg := testRunConfig(t.TempDir())
	cfg.ContentRules[0].ContentValue = "不存在的内容"
	res := Run(cfg, nil)
	if res.Success {
		t.Fatal("规则未匹配任何数据行，不应成功")
	}
	// 与 Python 版一致：规划期的失败只记入 details，不累计 failed_count
	if res.FailedCount != 0 {
		t.Fatalf("规划期失败不应计入 failed_count，实际 %d", res.FailedCount)
	}
	if len(res.Details) != 1 || res.Details[0].Status != "failed" {
		t.Fatalf("明细应含 1 条 failed：%+v", res.Details)
	}
	if !strings.Contains(res.Details[0].Note, "未匹配到任何数据行") {
		t.Fatalf("明细说明 = %q", res.Details[0].Note)
	}
}

func TestRunNumberRule(t *testing.T) {
	requireFile(t, testDataXLSX)
	cfg := testRunConfig(t.TempDir())
	cfg.ContentRules = nil
	cfg.NumberRules = []NumberRule{{
		Enabled:  true,
		RowStart: 4,
		RowEnd:   6,
		TemplateConfig: TemplateConfig{
			TemplateFolder:    testTplDir,
			TemplateMode:      "all",
			SelectedTemplates: []string{"1.模板-开工报告.docx"},
		},
	}}
	// 第 4-6 行为项目A开工、项目B开工、项目A验收 → 3 行 × 1 模板
	res := Run(cfg, nil)
	if !res.Success || res.Total != 3 || res.SuccessCount != 3 {
		t.Fatalf("res = %+v", res)
	}
	// 项目A 有两份（开工 + 验收），第二份自动加序号 (2)
	if _, err := os.Stat(filepath.Join(cfg.OutputFolder, "项目A", "项目A开工报告.docx")); err != nil {
		t.Errorf("缺少开工报告：%v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.OutputFolder, "项目A", "项目A开工报告 (2).docx")); err != nil {
		t.Errorf("缺少重名避让文件：%v", err)
	}
}

func TestConfigSaveLoad(t *testing.T) {
	cfg := testRunConfig(t.TempDir())
	cfg.Groups = map[string][]string{"a": {"b", "c"}}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, loaded) {
		t.Fatalf("往返不一致：\n原配置 %+v\n载入 %+v", cfg, loaded)
	}
	// JSON 文件本身合法
	var raw map[string]interface{}
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("config.json 非法 JSON：%v", err)
	}
	// 不存在的文件应报错
	if _, err := LoadConfig("不存在.json"); err == nil {
		t.Error("LoadConfig 对不存在文件应报错")
	}
}