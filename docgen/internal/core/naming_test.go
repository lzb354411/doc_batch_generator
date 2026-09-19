package core

// 命名相关函数测试：SanitizeFilename / TemplateDisplayName / BuildFilename /
// BuildFolderName / ResolveOutputPath。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"普通文件名", "项目A开工报告", "项目A开工报告"},
		{"非法字符全部替换为下划线", `a<b>c:d"e/f\g|h?i*j`, "a_b_c_d_e_f_g_h_i_j"},
		{"去首尾空白", "  前后空白  ", "前后空白"},
		{"去尾部句点", "末尾句点...", "末尾句点"},
		{"折叠连续空白为单个空格", "A\t B\nC", "A B C"},
		{"折叠全角空格", "项目\u3000A", "项目 A"}, // U+3000 属于 \p{Zs}
		{"去掉尾部句点和空白", "报告.  ", "报告"},
		{"全是句点", "...", ""},
	}
	for _, c := range cases {
		if got := SanitizeFilename(c.in); got != c.want {
			t.Errorf("%s：SanitizeFilename(%q) = %q，期望 %q", c.name, c.in, got, c.want)
		}
	}
}

func TestTemplateDisplayName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"序号前缀+模板前缀", "1.模板-开工报告.xlsx", "开工报告"},
		{"中文顿号序号", "10、模板-验收单.xlsx", "验收单"},
		{"连字符序号+模版", "2-模版-开工报告.docx", "开工报告"},
		{"只有模板前缀", "模板-通知.txt", "通知"},
		{"模版下划线前缀", "模版_清单.csv", "清单"},
		{"无前缀", "开工报告.xlsx", "开工报告"},
		{"多位数序号", "05.模板-测试.txt", "测试"},
		{"模板后无分隔符保留原名", "模板.txt", "模板"},
		{"序号后无模板前缀", "1.开工报告.xlsx", "开工报告"},
	}
	for _, c := range cases {
		if got := TemplateDisplayName(c.in); got != c.want {
			t.Errorf("%s：TemplateDisplayName(%q) = %q，期望 %q", c.name, c.in, got, c.want)
		}
	}
}

func TestBuildFilename(t *testing.T) {
	items := []NamingItem{{Type: "column", Key: "项目名称"}, {Type: "template"}}

	t.Run("列值+模板名叠加", func(t *testing.T) {
		fields := map[string]CellValue{"项目名称": cv("项目A")}
		got, errMsg := BuildFilename(items, fields, "1.模板-开工报告.docx")
		if errMsg != "" {
			t.Fatalf("不应报错：%s", errMsg)
		}
		if got != "项目A开工报告" {
			t.Fatalf("got %q，期望 %q", got, "项目A开工报告")
		}
	})

	t.Run("列值中的非法字符被净化", func(t *testing.T) {
		fields := map[string]CellValue{"项目名称": cv("A/B:C")}
		got, _ := BuildFilename(items, fields, "1.模板-开工报告.docx")
		if got != "A_B_C开工报告" {
			t.Fatalf("got %q，期望 %q", got, "A_B_C开工报告")
		}
	})

	t.Run("结果为空报错", func(t *testing.T) {
		// 全部命名项取不到值时结果为空（这里的列字段为空白）
		items := []NamingItem{{Type: "column", Key: "项目名称"}}
		fields := map[string]CellValue{"项目名称": cv("  ")}
		_, errMsg := BuildFilename(items, fields, "1.模板-开工报告.docx")
		if errMsg == "" || !strings.Contains(errMsg, "命名结果为空") {
			t.Fatalf("期望「命名结果为空」错误，实际：%q", errMsg)
		}
	})

	t.Run("超过255字符报错", func(t *testing.T) {
		fields := map[string]CellValue{"项目名称": cv(strings.Repeat("长", 300))}
		_, errMsg := BuildFilename(items, fields, "1.模板-开工报告.docx")
		if errMsg == "" || !strings.Contains(errMsg, "超过 255 上限") {
			t.Fatalf("期望「超过 255 上限」错误，实际：%q", errMsg)
		}
	})
}

func TestBuildFolderName(t *testing.T) {
	t.Run("跳过模板项", func(t *testing.T) {
		items := []NamingItem{{Type: "column", Key: "项目名称"}, {Type: "template"}}
		fields := map[string]CellValue{"项目名称": cv("项目A")}
		got, errMsg := BuildFolderName(items, fields)
		if errMsg != "" {
			t.Fatalf("不应报错：%s", errMsg)
		}
		if got != "项目A" {
			t.Fatalf("got %q，期望 %q", got, "项目A")
		}
	})

	t.Run("只有模板项时报错", func(t *testing.T) {
		items := []NamingItem{{Type: "template"}}
		_, errMsg := BuildFolderName(items, nil)
		if errMsg == "" || !strings.Contains(errMsg, "文件夹命名结果为空") {
			t.Fatalf("期望「文件夹命名结果为空」错误，实际：%q", errMsg)
		}
	})
}

func TestResolveOutputPath(t *testing.T) {
	dir := t.TempDir()
	used := map[string]bool{}
	base := filepath.Join(dir, "报告.docx")

	// 第一次：无重名，返回原名
	p1 := ResolveOutputPath(dir, "报告", ".docx", used)
	if p1 != base {
		t.Fatalf("第一次调用 = %q，期望 %q", p1, base)
	}
	// 第二次：同一次运行内重名，加 (2)
	p2 := ResolveOutputPath(dir, "报告", ".docx", used)
	if p2 != filepath.Join(dir, "报告 (2).docx") {
		t.Fatalf("第二次调用 = %q，期望带 (2)", p2)
	}
	// 第三次：加 (3)
	p3 := ResolveOutputPath(dir, "报告", ".docx", used)
	if p3 != filepath.Join(dir, "报告 (3).docx") {
		t.Fatalf("第三次调用 = %q，期望带 (3)", p3)
	}
}

func TestResolveOutputPathExistingFile(t *testing.T) {
	// 磁盘上已存在文件（例如上次生成的）也会触发重名避让
	dir := t.TempDir()
	existing := filepath.Join(dir, "清单.txt")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ResolveOutputPath(dir, "清单", ".txt", map[string]bool{})
	if got != filepath.Join(dir, "清单 (2).txt") {
		t.Fatalf("got %q，期望 %q", got, filepath.Join(dir, "清单 (2).txt"))
	}
}