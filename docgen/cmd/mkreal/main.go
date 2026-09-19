// mkreal —— 真实模板验收工具（开发文档 8.1 / 阶段 5「真实模板验收」）。
//
//	使用 e:\workgo\work\doc_batch_generator\templates 下的 20 个真实业务模板，
//	生成一份覆盖全部占位符列的数据表和 CLI 配置，跑一次完整生成并校验。
//
//	go run ./cmd/mkreal        生成 testdata/real/data.xlsx 与 config.json
//	go run . -config testdata/real/config.json   执行验收生成（输出到 testdata/real/out）
//	go run ./cmd/mkreal -verify   校验输出：文件数量与占位符替换结果
//
// 注意：当前工作目录必须是 docgen/（如 e:\workgo\work\doc_batch_generator\docgen）。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"docgen/internal/core"
	"github.com/xuri/excelize/v2"
)

var (
	realDir    = "testdata/real"
	dataPath   = filepath.Join(realDir, "data.xlsx")
	configPath = filepath.Join(realDir, "config.json")
	outDir     = filepath.Join(realDir, "out")
	verifyMode bool
)

// 真实模板目录：docgen/ 的上一级的 templates/（20 个 xlsx 业务表式模板）
func realTplDir() string {
	abs, err := filepath.Abs("../templates")
	if err != nil {
		panic(err)
	}
	return abs
}

func main() {
	flag.BoolVar(&verifyMode, "verify", false, "校验生成结果而非生成夹具")
	flag.Parse()
	var err error
	if verifyMode {
		err = verify()
	} else {
		err = generate()
	}
	if err != nil {
		fmt.Println("错误：", err)
		os.Exit(1)
	}
	if verifyMode {
		fmt.Println("真实模板验收：全部检查通过")
	}
}

// ---------- 生成数据表与配置 ----------

func generate() error {
	for _, d := range []string{realDir, outDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	if err := genDataXlsx(); err != nil {
		return err
	}

	tpls, err := scanTemplates()
	if err != nil {
		return err
	}
	absTpl := realTplDir()
	absExcel, _ := filepath.Abs(dataPath)
	absOut, _ := filepath.Abs(outDir)

	cfg := core.RunConfig{
		ExcelPath:       absExcel,
		SheetName:       "Sheet1",
		HeaderRow:       1,
		OutputFolder:    absOut,
		CreateSubfolder: true,
		NumberRules: []core.NumberRule{{
			Enabled:  true,
			RowStart: 2,
			RowEnd:   3,
			TemplateConfig: core.TemplateConfig{
				TemplateFolder:    absTpl,
				TemplateMode:      "all",
				SelectedTemplates: tpls,
			},
		}},
		NamingItems: []core.NamingItem{{Type: "column", Key: "项目名称"}, {Type: "template"}},
	}
	if err := core.SaveConfig(configPath, cfg); err != nil {
		return err
	}
	fmt.Printf("已生成：%s（%d 个项目）、%s（20 个模板全选）\n", dataPath, 2, configPath)
	return nil
}

// scanTemplates 列出真实模板目录下全部 xlsx（按文件名排序）。
func scanTemplates() ([]string, error) {
	entries, err := os.ReadDir(realTplDir())
	if err != nil {
		return nil, err
	}
	var tpls []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".xlsx") {
			tpls = append(tpls, e.Name())
		}
	}
	sort.Strings(tpls)
	if len(tpls) != 20 {
		return nil, fmt.Errorf("真实模板目录应含 20 个 xlsx，实际 %d", len(tpls))
	}
	return tpls, nil
}

// genDataXlsx 生成验收数据表：第 1 行标题，第 2-3 行两个项目，
// 覆盖全部 11 个占位符列（含工作票的安全措施勾选列与乡镇列）。
func genDataXlsx() error {
	f := excelize.NewFile()
	const sh = "Sheet1"
	for i, h := range []string{
		"项目名称", "项目定义", "开工时间", "竣工时间", "所属线路",
		"项目地址", "建设内容", "触电伤害", "物体打击", "起重作业", "乡镇",
	} {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sh, cell, h)
	}
	dateStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: strp("yyyy-mm-dd")})
	if err != nil {
		return err
	}
	projects := []struct {
		name, def, begin, end, line, addr, content, shock, strike, hoist, town string
		hasEnd                                                                 bool
	}{
		{
			name: "华欣路10kV配电工程", def: "新装变压器及配电柜",
			begin: "2026-03-01", end: "2026-06-30", hasEnd: true,
			line: "华欣Ⅰ线", addr: "华欣路88号",
			content: "新装500kVA变压器1台、低压配电柜2面",
			shock: "√", strike: "√", hoist: "", town: "城关镇",
		},
		{
			name: "东湖小区低压改造工程", def: "低压线路及接户线改造",
			begin: "2026-04-10", end: "", hasEnd: false,
			line: "东湖线", addr: "东湖小区1-12栋",
			content: "改造低压线路2.5km、更换接户线",
			shock: "", strike: "√", hoist: "√", town: "东湖街道",
		},
	}
	for i, p := range projects {
		row := 2 + i
		set := func(col int, v interface{}) {
			cell, _ := excelize.CoordinatesToCellName(col, row)
			f.SetCellValue(sh, cell, v)
		}
		set(1, p.name)
		set(2, p.def)
		setDate := func(col int, s string) {
			t, err := time.Parse("2006-01-02", s)
			if err != nil {
				return
			}
			cell, _ := excelize.CoordinatesToCellName(col, row)
			f.SetCellValue(sh, cell, t)
			f.SetCellStyle(sh, cell, cell, dateStyle)
		}
		setDate(3, p.begin)
		if p.hasEnd {
			setDate(4, p.end)
		}
		set(5, p.line)
		set(6, p.addr)
		set(7, p.content)
		set(8, p.shock)
		set(9, p.strike)
		set(10, p.hoist)
		set(11, p.town)
	}
	if err := f.SetSheetDimension(sh, "A1:K3"); err != nil {
		return err
	}
	return f.SaveAs(dataPath)
}

// ---------- 校验 ----------

var checksPass int

func check(name string, ok bool, detail string) {
	if !ok {
		fmt.Printf("[失败] %s —— %s\n", name, detail)
		os.Exit(1)
	}
	checksPass++
	fmt.Printf("[通过] %s\n", name)
}

func verify() error {
	if _, err := os.Stat(outDir); err != nil {
		return fmt.Errorf("输出目录不存在：%s（请先运行 go run . -config testdata/real/config.json）", outDir)
	}
	// 1. 每个项目一个子文件夹，各 20 个文件
	var folders []string
	entries, _ := os.ReadDir(outDir)
	for _, e := range entries {
		if e.IsDir() {
			folders = append(folders, e.Name())
		}
	}
	check(fmt.Sprintf("子文件夹 %d 个", len(folders)), len(folders) == 2, fmt.Sprint(folders))
	for _, fd := range folders {
		files, _ := os.ReadDir(filepath.Join(outDir, fd))
		var names []string
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".xlsx") {
				names = append(names, f.Name())
			}
		}
		check(fmt.Sprintf("%s 下 xlsx 数量 = %d", fd, len(names)), len(names) == 20, strings.Join(names, ", "))
	}

	// 2. 抽查：项目1 的开工报告含项目名称与日期；工作票含 √ 勾选
	// 输出文件名 = 项目名称 + 模板显示名（去序号前缀），按子串查找实际文件。
	proj1 := filepath.Join(outDir, "华欣路10kV配电工程")
	proj2 := filepath.Join(outDir, "东湖小区低压改造工程")
	f1, err := findFile(proj1, "开工报告")
	if err != nil {
		return err
	}
	check("开工报告已生成", f1 != "", f1)
	check("开工报告含项目名称", xlsxContains(f1, "华欣路10kV配电工程"), "")
	// 开工/竣工时间是整格日期占位符：写为真实日期数值并自动适配模板样式
	// （模板 yyyy"年"m"月"d"日" 显示 "2026年3月1日"），而非文本字符串。
	check("开工报告开工时间按模板格式显示", xlsxContains(f1, "2026年3月1日"), "")
	check("开工报告竣工时间按模板格式显示", xlsxContains(f1, "2026年6月30日"), "")
	check("开工报告开工时间为真实日期数值（非文本）", xlsxCellIsNumber(f1, "B4"), "")
	check("开工报告不含日期占位符", !xlsxContains(f1, "【开工时间】") && !xlsxContains(f1, "【竣工时间】"), "")

	f2, err := findFile(proj1, "工作票")
	if err != nil {
		return err
	}
	check("工作票已生成", f2 != "", f2)
	cnt, _ := xlsxCount(f2, "√")
	check("工作票安全措施勾选已替换（含 √）", cnt >= 2, fmt.Sprintf("√ 出现 %d 次", cnt))
	check("工作票含所属线路", xlsxContains(f2, "华欣Ⅰ线"), "")

	f3, err := findFile(proj2, "验收记录")
	if err != nil {
		return err
	}
	check("验收记录（项目2）含乡镇", f3 != "" && xlsxContains(f3, "东湖街道"), f3)

	return nil
}

// findFile 在文件夹中查找名称包含 substr 的 xlsx 文件（不存在返回 ""）。
func findFile(dir, substr string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".xlsx") && strings.Contains(e.Name(), substr) {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", nil
}

// xlsxContains 遍历工作簿所有单元格，任一单元格文本包含全部给定子串即返回 true。
func xlsxContains(path string, subs ...string) bool {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return false
	}
	defer f.Close()
	for _, sh := range f.GetSheetList() {
		rows, err := f.GetRows(sh)
		if err != nil {
			continue
		}
		for _, r := range rows {
			for _, v := range r {
				ok := true
				for _, s := range subs {
					if !strings.Contains(v, s) {
						ok = false
						break
					}
				}
				if ok && len(subs) > 0 {
					return true
				}
			}
		}
	}
	return false
}

// xlsxCount 统计所有单元格中 substr 的出现次数。
func xlsxCount(path, substr string) (int, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n := 0
	for _, sh := range f.GetSheetList() {
		rows, err := f.GetRows(sh)
		if err != nil {
			continue
		}
		for _, r := range rows {
			for _, v := range r {
				n += strings.Count(v, substr)
			}
		}
	}
	return n, nil
}

// xlsxCellIsNumber 检查首个工作表指定单元格的原始存储值为数值（说明是
// 真实日期序列号而非文本字符串）。
func xlsxCellIsNumber(path, cell string) bool {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return false
	}
	raw, err := f.GetCellValue(sheets[0], cell, excelize.Options{RawCellValue: true})
	if err != nil {
		return false
	}
	_, err = strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return err == nil
}

func strp(s string) *string { return &s }