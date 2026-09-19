package core

// docx 占位符替换测试：XML 扫描 / 实体转义 / 字节级替换 /
// 用 testdata/templates/1.模板-开工报告.docx 夹具做集成验证。

import (
	"bytes"
	"strings"
	"testing"
)

// docSample 复刻 mkfixtures 的 document.xml 结构：加粗首 run、
// 跨 run 拆分的占位符、xml:space 属性、未匹配占位符、无占位符段落。
const docSample = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:rPr><w:b/></w:rPr><w:t>项目：【项目名称】</w:t></w:r></w:p><w:p><w:r><w:t xml:space="preserve">日期（跨run拆分）：【</w:t></w:r><w:r><w:t>日期|YYYY年MM月DD日】</w:t></w:r></w:p><w:p><w:r><w:t>类型：【类型】 金额：【金额】</w:t></w:r></w:p><w:p><w:r><w:t>表格内：【项目名称】-【不存在的列】</w:t></w:r></w:p><w:p><w:r><w:t>结尾段落（无占位符，应保持不变）</w:t></w:r></w:p></w:body></w:document>`

func docFields() map[string]CellValue {
	return map[string]CellValue{
		"项目名称": cv("项目A"),
		"日期":   tm("2026-07-05"),
		"类型":   cv("开工"),
		"金额":   num(123.45),
	}
}

func TestFindTagEnd(t *testing.T) {
	// 属性值里的 '>' 不应误判为标签结束
	s := `<w:t xml:space="preserve">x</w:t>`
	from := strings.Index(s, "<")
	got := findTagEnd([]byte(s), from)
	if got != strings.Index(s, ">") {
		t.Fatalf("got %d，期望 %d", got, strings.Index(s, ">"))
	}
}

func TestScanNameEnd(t *testing.T) {
	if got := scanNameEnd([]byte("w:t>"), 0); got != 3 {
		t.Fatalf("scanNameEnd(w:t>) = %d", got)
	}
	if got := scanNameEnd([]byte("w:t "), 0); got != 3 {
		t.Fatalf("scanNameEnd(w:t ) = %d", got)
	}
}

func TestXmlEscape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc", "abc"},
		{`a&b<c>d`, "a&amp;b&lt;c&gt;d"},
	}
	for _, c := range cases {
		if got := xmlEscape(c.in); got != c.want {
			t.Errorf("xmlEscape(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestXmlUnescape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a&amp;b", "a&b"},
		{`a&lt;b&gt;c&quot;d&apos;e`, `a<b>c"d'e`},
		{"&#65;", "A"},
		{"&#x41;", "A"},
		{"&#20013;", "中"},
		{"&unknown;", "&unknown;"},  // 未知实体原样保留
		{"&#0;", "&#0;"},            // 非法字符引用原样保留
		{"a&b", "a&b"},              // 无分号不解析
		{"无实体", "无实体"},
	}
	for _, c := range cases {
		if got := xmlUnescape(c.in); got != c.want {
			t.Errorf("xmlUnescape(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestParseNumericRef(t *testing.T) {
	if r, ok := parseNumericRef("65"); !ok || r != 'A' {
		t.Error("十进制 65 应为 'A'")
	}
	if r, ok := parseNumericRef("x41"); !ok || r != 'A' {
		t.Error("十六进制 x41 应为 'A'")
	}
	if _, ok := parseNumericRef("d800"); ok {
		t.Error("代理区字符应判定无效")
	}
	if _, ok := parseNumericRef(""); ok {
		t.Error("空引用应判定无效")
	}
}

func TestScanDocxXml(t *testing.T) {
	paras, err := scanDocxXml([]byte(docSample))
	if err != nil {
		t.Fatal(err)
	}
	if len(paras) != 5 {
		t.Fatalf("段落数 = %d，期望 5", len(paras))
	}
	// 第二段（跨 run 拆分）应有 2 个 w:t 区间
	if len(paras[1]) != 2 {
		t.Fatalf("跨 run 段落 w:t 数 = %d，期望 2", len(paras[1]))
	}
	// 验证区间的文本内容
	sp := paras[1][0]
	if got := string([]byte(docSample)[sp.start:sp.end]); got != "日期（跨run拆分）：【" {
		t.Fatalf("首个 run 文本 = %q", got)
	}
}

func TestRewriteDocxXml(t *testing.T) {
	paras, err := scanDocxXml([]byte(docSample))
	if err != nil {
		t.Fatal(err)
	}
	out, unmatched := rewriteDocxXml([]byte(docSample), paras, docFields())
	s := string(out)

	checks := map[string]string{
		"首段替换且加粗 run 保留":           `<w:rPr><w:b/></w:rPr><w:t>项目：项目A</w:t>`,
		"跨 run 占位符合并替换":            `日期（跨run拆分）：2026年07月05日`,
		"第二 run 清空":                `<w:t></w:t>`,
		"xml:space 属性保留":            `xml:space="preserve">日期（跨run拆分）：2026年07月05日</w:t>`,
		"金额小数渲染":                  `类型：开工 金额：123.45`,
		"表格内替换且未匹配原样保留":           `表格内：项目A-【不存在的列】`,
		"无占位符段落保持不变":              `结尾段落（无占位符，应保持不变）`,
	}
	for name, want := range checks {
		if !strings.Contains(s, want) {
			t.Errorf("%s：输出缺少 %q", name, want)
		}
	}
	// 未匹配占位符只剩 1 对括号
	if c := strings.Count(s, "【"); c != 1 {
		t.Errorf("剩余 【 数量 = %d，期望 1", c)
	}
	// 未匹配列表
	found := false
	for _, u := range unmatched {
		if u == "不存在的列" {
			found = true
		}
	}
	if !found {
		t.Errorf("unmatched = %v，应包含「不存在的列」", unmatched)
	}
}

func TestReplaceDocxPlaceholdersFixture(t *testing.T) {
	requireFile(t, testTplDir+"/1.模板-开工报告.docx")
	out := t.TempDir() + "/out.docx"
	unmatched, err := ReplaceDocxPlaceholders(testTplDir+"/1.模板-开工报告.docx", out, docFields())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range unmatched {
		if u == "不存在的列" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unmatched = %v，应包含「不存在的列」", unmatched)
	}
	s, err := readDocxEntry(out, "word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(s), "项目：项目A") {
		t.Error("输出缺少替换后的项目名")
	}
	if !strings.Contains(string(s), "日期（跨run拆分）：2026年07月05日") {
		t.Error("输出缺少跨 run 替换后的日期")
	}
	// 其余 zip 部件（[Content_Types].xml）必须逐字节保留
	tplCT, _ := readDocxEntry(testTplDir+"/1.模板-开工报告.docx", "[Content_Types].xml")
	outCT, _ := readDocxEntry(out, "[Content_Types].xml")
	if !bytes.Equal(tplCT, outCT) {
		t.Error("[Content_Types].xml 未被修改但内容不一致")
	}
}

func TestDocxPlaceholdersFixture(t *testing.T) {
	requireFile(t, testTplDir+"/1.模板-开工报告.docx")
	names, err := DocxPlaceholders(testTplDir + "/1.模板-开工报告.docx")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"项目名称", "日期", "类型", "金额", "不存在的列"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("names = %v，期望 %v", names, want)
	}
}