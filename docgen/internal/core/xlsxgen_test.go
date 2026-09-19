package core

// 模板生成测试：txt/csv/GBK/xlsx 占位符替换、按扩展名分发、
// resultMsg / normalizeNewlines，以及生成前占位符提取。
// 复用 testdata/templates/ 下的 5 个模板夹具。

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestNormalizeNewlines(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a\r\nb", "a\nb"},
		{"a\rb", "a\nb"},
		{"x\r\ny\rz", "x\ny\nz"},
		{"无换行", "无换行"},
	}
	for _, c := range cases {
		if got := normalizeNewlines(c.in); got != c.want {
			t.Errorf("normalizeNewlines(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestResultMsg(t *testing.T) {
	if ok, msg := resultMsg(nil, nil); !ok || msg != "已生成" {
		t.Fatalf("无未匹配 = %v, %q", ok, msg)
	}
	if ok, msg := resultMsg(nil, errors.New("保存失败")); ok || msg != "保存失败" {
		t.Fatalf("有错误 = %v, %q", ok, msg)
	}
	// 去重 + 排序
	if _, msg := resultMsg([]string{"B", "A", "B"}, nil); msg != "已生成（未匹配占位符：A、B）" {
		t.Fatalf("去重排序 = %q", msg)
	}
	// 超过 5 个只保留前 5 个
	six := []string{"f", "e", "d", "c", "b", "a"}
	if _, msg := resultMsg(six, nil); msg != "已生成（未匹配占位符：a、b、c、d、e）" {
		t.Fatalf("截断 = %q", msg)
	}
}

// textFixtureFields 按项目构造字段，与 mkfixtures 数据表一致。
func textFixtureFields(project, typ string) map[string]CellValue {
	return map[string]CellValue{
		"项目名称": cv(project),
		"类型":   cv(typ),
		"日期":   tm("2026-09-01"),
		"编号":   num(1005),
		"金额":   num(100),
	}
}

func TestReplaceTextPlaceholdersUTF8(t *testing.T) {
	requireFile(t, testTplDir+"/3.模板-通知.txt")
	out := filepath.Join(t.TempDir(), "通知.txt")
	fields := textFixtureFields("项目D", "验收")
	unmatched, err := ReplaceTextPlaceholders(testTplDir+"/3.模板-通知.txt", out, fields)
	if err != nil {
		t.Fatal(err)
	}
	if len(unmatched) != 1 || unmatched[0] != "不存在的列" {
		t.Fatalf("unmatched = %v", unmatched)
	}
	data, _ := os.ReadFile(out)
	s := string(data)
	for _, want := range []string{"项目D通知", "类型：验收", "日期：2026年09月01日", "未匹配：【不存在的列】"} {
		if !strings.Contains(s, want) {
			t.Errorf("输出缺少 %q", want)
		}
	}
	// 换行统一为 \r\n：\r\n 的个数应等于 \n 的个数（不允许孤立 \n）
	if strings.Count(s, "\r\n") != strings.Count(s, "\n") {
		t.Errorf("存在孤立 LF：%q", s)
	}
}

func TestReplaceTextPlaceholdersGBK(t *testing.T) {
	requireFile(t, testTplDir+"/5.模板-GBK测试.txt")
	out := filepath.Join(t.TempDir(), "gbk.txt")
	fields := map[string]CellValue{"项目名称": cv("项目A"), "类型": cv("开工")}
	if _, err := ReplaceTextPlaceholders(testTplDir+"/5.模板-GBK测试.txt", out, fields); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	// 输出为 UTF-8，GBK 模板解码正确，CRLF 换行
	if string(data) != "项目AGBK测试\r\n类型：开工\r\n" {
		t.Fatalf("GBK 输出 = %q", string(data))
	}
}

func TestReplaceXlsxPlaceholdersFixture(t *testing.T) {
	requireFile(t, testTplDir + "/2.模板-验收单.xlsx")
	out := filepath.Join(t.TempDir(), "验收单.xlsx")
	fields := map[string]CellValue{
		"项目名称": cv("项目B"),
		"类型":   cv("开工"),
		"日期":   tm("2026-08-15"),
		"编号":   num(1002),
		"金额":   num(6789),
	}
	_, err := ReplaceXlsxPlaceholders(testTplDir+"/2.模板-验收单.xlsx", out, fields)
	if err != nil {
		t.Fatal(err)
	}
	xf, err := excelize.OpenFile(out)
	if err != nil {
		t.Fatal(err)
	}
	defer xf.Close()
	get := func(sheet, cell string) string {
		v, _ := xf.GetCellValue(sheet, cell)
		return v
	}
	if get("Sheet1", "A2") != "项目：项目B" || get("Sheet1", "B2") != "类型：开工" {
		t.Errorf("Sheet1 A2/B2 = %q / %q", get("Sheet1", "A2"), get("Sheet1", "B2"))
	}
	if get("Sheet1", "A3") != "日期：2026/08/15" {
		t.Errorf("A3 = %q", get("Sheet1", "A3"))
	}
	if get("Sheet1", "A4") != "编号：1002 金额：6789" {
		t.Errorf("A4 = %q", get("Sheet1", "A4"))
	}
	// 无占位符的日期单元格保持不动
	if get("Sheet1", "C1") != "2026-07-30" {
		t.Errorf("C1 = %q", get("Sheet1", "C1"))
	}
	// 第二个工作表也替换；未匹配的保留下括号
	if get("明细", "A1") != "明细：项目B" || get("明细", "B1") != "未匹配：【不存在的列】" {
		t.Errorf("明细 = %q / %q", get("明细", "A1"), get("明细", "B1"))
	}
	// 样式保留（A1 加粗）
	sid, _ := xf.GetCellStyle("Sheet1", "A1")
	st, err := xf.GetStyle(sid)
	if err != nil || st.Font == nil || !st.Font.Bold {
		t.Error("A1 加粗样式未保留")
	}
}

func TestGenerateFile(t *testing.T) {
	t.Run("不支持的扩展名", func(t *testing.T) {
		ok, msg := generateFile("模板.xyz", filepath.Join(t.TempDir(), "out"), nil)
		if ok || !strings.Contains(msg, "不支持的模板类型") {
			t.Fatalf("ok=%v msg=%q", ok, msg)
		}
	})

	t.Run("txt 模板端到端分发", func(t *testing.T) {
		requireFile(t, testTplDir+"/3.模板-通知.txt")
		out := filepath.Join(t.TempDir(), "out.txt")
		fields := textFixtureFields("项目C", "开工")
		ok, msg := generateFile(testTplDir+"/3.模板-通知.txt", out, fields)
		if !ok || msg != "已生成（未匹配占位符：不存在的列）" {
			t.Fatalf("ok=%v msg=%q", ok, msg)
		}
		data, _ := os.ReadFile(out)
		if !strings.Contains(string(data), "项目C通知") {
			t.Error("输出缺少内容")
		}
	})
}

func TestTemplatePlaceholders(t *testing.T) {
	requireFile(t, testTplDir)
	cases := []struct {
		name string
		tpl  string
		want []string
	}{
		{"docx", "1.模板-开工报告.docx", []string{"项目名称", "日期", "类型", "金额", "不存在的列"}},
		{"xlsx", "2.模板-验收单.xlsx", []string{"项目名称", "类型", "日期", "编号", "金额", "不存在的列"}},
		{"txt", "3.模板-通知.txt", []string{"项目名称", "类型", "日期", "金额", "不存在的列"}},
		{"csv", "4.模板-清单.csv", []string{"项目名称", "类型", "编号", "日期", "金额"}},
		{"gbk", "5.模板-GBK测试.txt", []string{"项目名称", "类型"}},
	}
	for _, c := range cases {
		names, err := TemplatePlaceholders(testTplDir + "/" + c.tpl)
		if err != nil {
			t.Errorf("%s：%v", c.name, err)
			continue
		}
		if strings.Join(names, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s：names = %v，期望 %v", c.name, names, c.want)
		}
	}
	if _, err := TemplatePlaceholders("x.pdf"); err == nil {
		t.Error("不支持的模板类型应报错")
	}
}

// TestReplaceXlsxPlaceholdersDateCells 验证「整格日期占位符」写入真实日期值：
//   - 无样式 → 默认显示为 yyyy/mm/dd；
//   - 模板单元格已有日期格式 → 保留模板格式（自动匹配）；
//   - 显式格式（|YYYY/MM/DD、|YYYY年MM月DD日）→ 转为 Excel 数字格式；
//   - 混合文本（"日期：【日期】"）→ 仍按字符串输出。
func TestReplaceXlsxPlaceholdersDateCells(t *testing.T) {
	f := excelize.NewFile()
	const sh = "Sheet1"
	f.SetCellValue(sh, "A1", "【日期】")                     // 无样式 → 默认 yyyy/mm/dd
	f.SetCellValue(sh, "B1", "【日期】")                     // 模板已有日期格式 → 自动匹配
	f.SetCellValue(sh, "C1", "【日期|YYYY/MM/DD】")          // 显式格式 → 转 Excel 数字格式
	f.SetCellValue(sh, "D1", "日期：【日期】")                 // 混合文本 → 字符串
	f.SetCellValue(sh, "E1", "【日期|YYYY年MM月DD日】")        // 中文显式格式
	dateStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: strPtr("yyyy-mm-dd")})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellStyle(sh, "B1", "B1", dateStyle); err != nil {
		t.Fatal(err)
	}
	if err := f.SetSheetDimension(sh, "A1:E1"); err != nil {
		t.Fatal(err)
	}
	tpl := filepath.Join(t.TempDir(), "date-tpl.xlsx")
	if err := f.SaveAs(tpl); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "date-out.xlsx")
	fields := map[string]CellValue{"日期": tm("2026-09-03")}
	if _, err := ReplaceXlsxPlaceholders(tpl, out, fields); err != nil {
		t.Fatal(err)
	}
	xf, err := excelize.OpenFile(out)
	if err != nil {
		t.Fatal(err)
	}
	defer xf.Close()
	fmtVal := func(cell string) string {
		v, _ := xf.GetCellValue(sh, cell)
		return v
	}
	if got := fmtVal("A1"); got != "2026/09/03" {
		t.Errorf("A1 = %q，期望 2026/09/03（默认日期格式）", got)
	}
	if got := fmtVal("B1"); got != "2026-09-03" {
		t.Errorf("B1 = %q，期望 2026-09-03（自动匹配模板日期格式）", got)
	}
	if got := fmtVal("C1"); got != "2026/09/03" {
		t.Errorf("C1 = %q，期望 2026/09/03（显式格式转数字格式）", got)
	}
	if got := fmtVal("D1"); got != "日期：2026/09/03" {
		t.Errorf("D1 = %q，期望 日期：2026/09/03（混合文本）", got)
	}
	if got := fmtVal("E1"); got != "2026年09月03日" {
		t.Errorf("E1 = %q，期望 2026年09月03日（中文显式格式）", got)
	}
	// 写入的是真实日期（数值型序列号），而非文本：
	// 原始值应为可解析为数字的序列号；D1 为混合文本，原始值仍是字符串。
	rawVal := func(cell string) string {
		v, _ := xf.GetCellValue(sh, cell, excelize.Options{RawCellValue: true})
		return v
	}
	for _, cell := range []string{"A1", "B1", "C1", "E1"} {
		if _, err := strconv.ParseFloat(rawVal(cell), 64); err != nil {
			t.Errorf("%s 应为真实日期（数值），原始值 %q 不是数字", cell, rawVal(cell))
		}
	}
	if _, err := strconv.ParseFloat(rawVal("D1"), 64); err == nil {
		t.Error("D1 为混合文本，其原始值不应是数字")
	}
}

// TestDateFormatToNumFmt 验证日期令牌格式到 Excel 数字格式代码的翻译。
func TestDateFormatToNumFmt(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"YYYY/MM/DD", "yyyy/mm/dd", true},
		{"YYYY-MM-DD", "yyyy-mm-dd", true},
		{"YYYY年MM月DD日", "yyyy年mm月dd日", true}, // 中文为字面量，不加引号 Excel 亦按字面显示
		{"YYYY/MM/DD HH:mm:ss", "yyyy/mm/dd hh:mm:ss", true},
		{"YY年MM月", "yy年mm月", true},
		{"立即执行", "", false}, // 不含日期令牌 → 不可用（回退文本，与 Word 一致）
		{`带"引号`, "", false},  // 含引号 → 不可用
	}
	for _, c := range cases {
		got, ok := dateFormatToNumFmt(c.in)
		if ok != c.ok || (c.ok && got != c.want) {
			t.Errorf("dateFormatToNumFmt(%q) = %q, %v，期望 %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}