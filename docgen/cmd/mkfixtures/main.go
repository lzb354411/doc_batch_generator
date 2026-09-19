// mkfixtures —— 阶段 1 验证工具：生成测试夹具并校验 docgen 输出。
//
//	go run ./cmd/mkfixtures          生成 testdata/ 下的数据表与模板
//	go run ./cmd/mkfixtures -verify  校验 testdata/out 下的生成结果
package main

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
)

var (
	baseDir     = "testdata"
	dataPath    = filepath.Join(baseDir, "data.xlsx")
	tplDir      = filepath.Join(baseDir, "templates")
	outDir      = filepath.Join(baseDir, "out")
	verifyMode  bool
	checksRun   int
	checksFail  int
)

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`

const relsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`

// document.xml 特意包含：加粗首 run、跨 run 拆分的占位符、xml:space 属性、
// 表格内段落、未匹配占位符、无占位符段落。
const documentXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:rPr><w:b/></w:rPr><w:t>项目：【项目名称】</w:t></w:r></w:p><w:p><w:r><w:t xml:space="preserve">日期（跨run拆分）：【</w:t></w:r><w:r><w:t>日期|YYYY年MM月DD日】</w:t></w:r></w:p><w:p><w:r><w:t>类型：【类型】 金额：【金额】</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>表格内：【项目名称】-【不存在的列】</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:p><w:r><w:t>结尾段落（无占位符，应保持不变）</w:t></w:r></w:p></w:body></w:document>`

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
		fmt.Printf("\n校验完成：%d 项检查，%d 失败\n", checksRun, checksFail)
		if checksFail > 0 {
			os.Exit(1)
		}
	}
}

func generate() error {
	for _, d := range []string{tplDir, outDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	if err := genDataXlsx(); err != nil {
		return err
	}
	if err := genTplXlsx(); err != nil {
		return err
	}
	if err := genTplDocx(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tplDir, "3.模板-通知.txt"), []byte(
		"【项目名称】通知\n类型：【类型】\n日期：【日期|YYYY年MM月DD日】\n金额：【金额】\n未匹配：【不存在的列】\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tplDir, "4.模板-清单.csv"), []byte(
		"项目名称,类型,编号\n【项目名称】,【类型】,【编号】\n日期：【日期|YYYY-MM-DD】,金额：【金额】,固定列\n"), 0o644); err != nil {
		return err
	}
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("【项目名称】GBK测试\n类型：【类型】\n"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tplDir, "5.模板-GBK测试.txt"), gbk, 0o644); err != nil {
		return err
	}
	fmt.Println("夹具已生成：", dataPath, "|", tplDir)
	return nil
}

// genDataXlsx 生成数据表：标题在第 3 行，第 2 行空白，第 8 行空白，
// 第 10-11 行为尾部空行（应被截断），并含日期、整数、小数列。
func genDataXlsx() error {
	f := excelize.NewFile()
	const sh = "Sheet1"
	f.SetCellValue(sh, "A1", "说明：本表为测试数据（标题在第 3 行）")
	// 第 2 行留空
	for i, h := range []string{"项目名称", "类型", "日期", "编号", "金额"} {
		cell, _ := excelize.CoordinatesToCellName(i+1, 3)
		f.SetCellValue(sh, cell, h)
	}
	type row struct {
		name, typ string
		date      time.Time
		hasDate   bool
		num, amt  float64
	}
	rows := []row{
		{"项目A", "开工", time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC), true, 1001, 123.45},
		{"项目B", "开工", time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), true, 1002, 6789},
		{"项目A", "验收", time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), true, 1003, 0.5},
		{"项目C", "开工", time.Time{}, false, 1004, 42},
		{}, // 第 8 行空白
		{"项目D", "验收", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), true, 1005, 100},
	}
	dateStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: strPtr("yyyy-mm-dd")})
	if err != nil {
		return err
	}
	for i, r := range rows {
		rowNum := 4 + i
		if r.name == "" && r.typ == "" {
			continue
		}
		set := func(col int, v interface{}) {
			cell, _ := excelize.CoordinatesToCellName(col, rowNum)
			f.SetCellValue(sh, cell, v)
		}
		set(1, r.name)
		set(2, r.typ)
		if r.hasDate {
			set(3, r.date)
			cell, _ := excelize.CoordinatesToCellName(3, rowNum)
			f.SetCellStyle(sh, cell, cell, dateStyle)
		}
		set(4, r.num)
		set(5, r.amt)
	}
	f.NewSheet("汇总")
	f.SetCellValue("汇总", "A1", "备注")
	f.SetCellValue("汇总", "B1", "这是第二个 Sheet")
	// excelize 不会自动更新 dimension（保持模板默认 A1），
	// 而 openpyxl read_only 模式依赖 dimension 读取，真实 Excel 文件均有正确值
	if err := f.SetSheetDimension(sh, "A1:E9"); err != nil {
		return err
	}
	if err := f.SetSheetDimension("汇总", "A1:B1"); err != nil {
		return err
	}
	return f.SaveAs(dataPath)
}

// genTplXlsx 生成 xlsx 模板：两个工作表、占位符、样式与日期单元格（应保持不动）。
func genTplXlsx() error {
	f := excelize.NewFile()
	f.SetCellValue("Sheet1", "A1", "验收单")
	f.SetCellValue("Sheet1", "A2", "项目：【项目名称】")
	f.SetCellValue("Sheet1", "B2", "类型：【类型】")
	f.SetCellValue("Sheet1", "A3", "日期：【日期|YYYY/MM/DD】")
	f.SetCellValue("Sheet1", "A4", "编号：【编号】 金额：【金额】")
	titleStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}})
	f.SetCellStyle("Sheet1", "A1", "A1", titleStyle)
	dateStyle, _ := f.NewStyle(&excelize.Style{CustomNumFmt: strPtr("yyyy-mm-dd")})
	f.SetCellValue("Sheet1", "C1", time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC))
	f.SetCellStyle("Sheet1", "C1", "C1", dateStyle)
	f.NewSheet("明细")
	f.SetCellValue("明细", "A1", "明细：【项目名称】")
	f.SetCellValue("明细", "B1", "未匹配：【不存在的列】")
	if err := f.SetSheetDimension("Sheet1", "A1:C4"); err != nil {
		return err
	}
	if err := f.SetSheetDimension("明细", "A1:B1"); err != nil {
		return err
	}
	return f.SaveAs(filepath.Join(tplDir, "2.模板-验收单.xlsx"))
}

func genTplDocx() error {
	out, err := os.Create(filepath.Join(tplDir, "1.模板-开工报告.docx"))
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	for name, content := range map[string]string{
		"[Content_Types].xml": contentTypesXML,
		"_rels/.rels":         relsXML,
		"word/document.xml":   documentXML,
	} {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			return err
		}
	}
	return zw.Close()
}

// ---------- 校验 ----------

func check(name string, ok bool, detail string) {
	checksRun++
	mark := "通过"
	if !ok {
		mark = "失败"
		checksFail++
	}
	fmt.Printf("[%s] %s", mark, name)
	if !ok && detail != "" {
		fmt.Printf(" —— %s", detail)
	}
	fmt.Println()
}

func verify() error {
	// 1. docx 输出
	doc, err := readZipEntry(filepath.Join(outDir, "项目A", "项目A开工报告.docx"), "word/document.xml")
	if err != nil {
		return fmt.Errorf("读取输出 docx 失败：%w", err)
	}
	s := string(doc)
	check("docx 首段替换 + 加粗 run 保留", strings.Contains(s, "<w:rPr><w:b/></w:rPr><w:t>项目：项目A</w:t>"), "")
	check("docx 跨 run 占位符合并替换", strings.Contains(s, `日期（跨run拆分）：2026年07月05日`), "")
	check("docx 清空了第二 run", strings.Contains(s, "<w:t></w:t>"), "")
	check("docx xml:space 属性保留", strings.Contains(s, `xml:space="preserve">日期（跨run拆分）：2026年07月05日</w:t>`), "")
	check("docx 金额小数渲染", strings.Contains(s, "类型：开工 金额：123.45"), "")
	check("docx 表格内替换 + 未匹配保留", strings.Contains(s, "表格内：项目A-【不存在的列】"), "")
	check("docx 无占位符段落保持不变", strings.Contains(s, "结尾段落（无占位符，应保持不变）"), "")
	check("docx 未匹配占位符不吞失", strings.Count(s, "【") == 1 && strings.Count(s, "】") == 1, "剩余括号数不对")
	// 模板其余部件逐字节保留
	tplCT, _ := readZipEntry(filepath.Join(tplDir, "1.模板-开工报告.docx"), "[Content_Types].xml")
	outCT, _ := readZipEntry(filepath.Join(outDir, "项目A", "项目A开工报告.docx"), "[Content_Types].xml")
	check("docx 其他 zip 部件原样保留", bytes.Equal(tplCT, outCT), "")

	// 2. xlsx 输出（项目B：日期 2026-08-15、金额 6789 整数渲染）
	xf, err := excelize.OpenFile(filepath.Join(outDir, "项目B", "项目B验收单.xlsx"))
	if err != nil {
		return fmt.Errorf("读取输出 xlsx 失败：%w", err)
	}
	defer xf.Close()
	xget := func(sheet, cell string) string {
		v, _ := xf.GetCellValue(sheet, cell)
		return v
	}
	check("xlsx 替换（Sheet1）", xget("Sheet1", "A2") == "项目：项目B" && xget("Sheet1", "B2") == "类型：开工", "")
	check("xlsx 日期格式化", xget("Sheet1", "A3") == "日期：2026/08/15", xget("Sheet1", "A3"))
	check("xlsx 整数金额渲染", xget("Sheet1", "A4") == "编号：1002 金额：6789", xget("Sheet1", "A4"))
	check("xlsx 第二工作表替换 + 未匹配保留", xget("明细", "A1") == "明细：项目B" && xget("明细", "B1") == "未匹配：【不存在的列】", "")
	check("xlsx 无占位符日期单元格保持", xget("Sheet1", "C1") == "2026-07-30", xget("Sheet1", "C1"))
	check("xlsx 标题样式保留（加粗）", func() bool {
		sid, _ := xf.GetCellStyle("Sheet1", "A1")
		st, err := xf.GetStyle(sid)
		return err == nil && st.Font != nil && st.Font.Bold
	}(), "")

	// 3. csv 输出（项目C：空白日期）
	csv, err := os.ReadFile(filepath.Join(outDir, "项目C", "项目C清单.csv"))
	if err != nil {
		return fmt.Errorf("读取输出 csv 失败：%w", err)
	}
	cs := string(csv)
	check("csv 替换", strings.Contains(cs, "项目C,开工,1004"), cs)
	check("csv 空白日期渲染为空", strings.Contains(cs, "日期：,金额：42,固定列"), cs)

	// 4. txt 输出（UTF-8 模板）
	txt, err := os.ReadFile(filepath.Join(outDir, "项目D", "项目D通知.txt"))
	if err != nil {
		return fmt.Errorf("读取输出 txt 失败：%w", err)
	}
	check("txt 替换 + 日期格式化 + 未匹配保留",
		strings.Contains(string(txt), "项目D通知") &&
			strings.Contains(string(txt), "日期：2026年09月01日") &&
			strings.Contains(string(txt), "未匹配：【不存在的列】"), string(txt))

	// 5. GBK 模板输出（写出为 UTF-8）
	gtxt, err := os.ReadFile(filepath.Join(outDir, "项目A", "项目AGBK测试.txt"))
	if err != nil {
		return fmt.Errorf("读取 GBK 输出失败：%w", err)
	}
	check("GBK 模板读取并替换（输出 UTF-8，CRLF 换行）",
		string(gtxt) == "项目AGBK测试\r\n类型：开工\r\n", string(gtxt))

	// 6. 子文件夹与重名序号
	for _, d := range []string{"项目A", "项目B", "项目C", "项目D"} {
		st, err := os.Stat(filepath.Join(outDir, d))
		check("子文件夹："+d, err == nil && st.IsDir(), "")
	}
	_, err = os.Stat(filepath.Join(outDir, "项目A", "项目A通知 (2).txt"))
	check("重名自动加序号 (2)", err == nil, "")
	return nil
}

func readZipEntry(path, entry string) ([]byte, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != entry {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("zip 中找不到 %s", entry)
}

func strPtr(s string) *string { return &s }
