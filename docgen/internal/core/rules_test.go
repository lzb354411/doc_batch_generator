package core

// 规则与选行函数测试：BuildHeaderMap / RowToFields / IsEmptyRow /
// TruncateTrailingEmptyRows / SelectContentRows / SelectNumberRows /
// ResolveTemplates。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testRows 构造一张 5 行的虚拟表格（第 1 行 = 标题，第 4 行为空行）。
func testRows() [][]CellValue {
	return [][]CellValue{
		{cv("名称"), cv("类型")},        // 第 1 行：标题
		{cv("A"), cv("开工")},           // 第 2 行
		{cv("B"), cv("验收")},           // 第 3 行
		{},                             // 第 4 行：空行
		{cv("C"), cv("开工")},           // 第 5 行
	}
}

func TestBuildHeaderMap(t *testing.T) {
	rows := testRows()
	colMap, names := BuildHeaderMap(rows, 1)
	if len(colMap) != 2 || colMap["名称"] != 0 || colMap["类型"] != 1 {
		t.Fatalf("colMap = %v", colMap)
	}
	if len(names) != 2 || names[0] != "名称" || names[1] != "类型" {
		t.Fatalf("names = %v", names)
	}
	if m, _ := BuildHeaderMap(rows, 0); len(m) != 0 {
		t.Error("标题行 0 应返回空映射")
	}
	if m, _ := BuildHeaderMap(rows, 99); len(m) != 0 {
		t.Error("超出范围的标题行应返回空映射")
	}
}

func TestBuildHeaderMapDuplicateColumn(t *testing.T) {
	// 重复列名只取首个
	rows := [][]CellValue{
		{cv("名称"), cv("名称"), cv("类型")},
	}
	colMap, names := BuildHeaderMap(rows, 1)
	if colMap["名称"] != 0 || len(names) != 2 {
		t.Fatalf("colMap = %v, names = %v", colMap, names)
	}
}

func TestRowToFields(t *testing.T) {
	rows := testRows()
	colMap, _ := BuildHeaderMap(rows, 1)
	fields := RowToFields(rows[1], colMap)
	if fields["名称"].String() != "A" || fields["类型"].String() != "开工" {
		t.Fatalf("fields = %v", fields)
	}
	// 行比列短时缺失列返回空值，不应 panic
	fields = RowToFields(rows[3], colMap)
	if !fields["名称"].Empty() {
		t.Fatalf("缺失列应为空 CellValue，实际 %v", fields["名称"])
	}
}

func TestIsEmptyRow(t *testing.T) {
	rows := testRows()
	if IsEmptyRow(rows[0]) || IsEmptyRow(rows[1]) {
		t.Error("非空行不应判为空")
	}
	if !IsEmptyRow(rows[3]) {
		t.Error("空行应判为空")
	}
	if !IsEmptyRow(nil) {
		t.Error("nil 行应判为空")
	}
	if !IsEmptyRow([]CellValue{{Str: "  "}}) {
		t.Error("纯空白行应判为空")
	}
}

func TestTruncateTrailingEmptyRows(t *testing.T) {
	rows := append(testRows(), []CellValue{}, nil)
	got := TruncateTrailingEmptyRows(rows)
	if len(got) != 5 {
		t.Fatalf("截断后 = %d 行，期望 5", len(got))
	}
	if got := TruncateTrailingEmptyRows([][]CellValue{{}, {}}); got != nil {
		t.Fatal("全空表应返回 nil")
	}
}

func TestSelectContentRows(t *testing.T) {
	rows := testRows()
	colMap, _ := BuildHeaderMap(rows, 1)
	// 从标题行后一行的 0 基索引（headerRow）开始扫描，跳过空行
	got := SelectContentRows(rows, 1, colMap, "类型", "开工")
	want := []int{1, 4}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v，期望 %v", got, want)
	}
	if got := SelectContentRows(rows, 1, colMap, "类型", "不存在"); got != nil {
		t.Fatalf("无匹配应返回 nil，实际 %v", got)
	}
	if got := SelectContentRows(rows, 1, colMap, "不存在的列", "x"); got != nil {
		t.Fatalf("列不存在应返回 nil，实际 %v", got)
	}
}

func TestSelectNumberRows(t *testing.T) {
	rows := testRows()
	// 0 基结果：第 2-3 行 → 索引 1,2
	got := SelectNumberRows(rows, 2, 3)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("got %v，期望 [1 2]", got)
	}
	// 空行（第 4 行）被跳过
	got = SelectNumberRows(rows, 4, 4)
	if got != nil {
		t.Fatalf("空行应被跳过，实际 %v", got)
	}
	// 范围自动裁剪：start < 1 视为 1，end 超过行数视为最后一行
	got = SelectNumberRows(rows, 0, 99)
	if len(got) != 4 { // 5 行 → 索引 0,1,2,4（去掉空行）
		t.Fatalf("got %v，期望 4 行", got)
	}
}

func TestResolveTemplates(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("按勾选模板", func(t *testing.T) {
		tc := TemplateConfig{TemplateFolder: dir, TemplateMode: "all",
			SelectedTemplates: []string{"a.txt", "b.txt"}}
		names, errMsg := ResolveTemplates(tc, nil, "")
		if errMsg != "" || len(names) != 2 {
			t.Fatalf("names = %v, err = %q", names, errMsg)
		}
	})

	t.Run("默认目录兜底", func(t *testing.T) {
		tc := TemplateConfig{TemplateMode: "all", SelectedTemplates: []string{"a.txt"}}
		names, errMsg := ResolveTemplates(tc, nil, dir)
		if errMsg != "" || len(names) != 1 {
			t.Fatalf("names = %v, err = %q", names, errMsg)
		}
	})

	t.Run("按分组展开去重保序", func(t *testing.T) {
		groups := map[string][]string{"g1": {"a.txt", "b.txt"}, "g2": {"b.txt"}}
		tc := TemplateConfig{TemplateMode: "group", SelectedGroups: []string{"g1", "g2"}}
		names, errMsg := ResolveTemplates(tc, groups, dir)
		if errMsg != "" || len(names) != 2 || names[0] != "a.txt" || names[1] != "b.txt" {
			t.Fatalf("names = %v, err = %q", names, errMsg)
		}
	})

	t.Run("未选择模板报错", func(t *testing.T) {
		tc := TemplateConfig{TemplateFolder: dir}
		_, errMsg := ResolveTemplates(tc, nil, "")
		if errMsg == "" || !strings.Contains(errMsg, "未选择任何模板") {
			t.Fatalf("err = %q", errMsg)
		}
	})

	t.Run("模板缺失报错", func(t *testing.T) {
		tc := TemplateConfig{TemplateFolder: dir, TemplateMode: "all",
			SelectedTemplates: []string{"缺失.txt"}}
		_, errMsg := ResolveTemplates(tc, nil, "")
		if errMsg == "" || !strings.Contains(errMsg, "模板文件夹中找不到") {
			t.Fatalf("err = %q", errMsg)
		}
	})
}