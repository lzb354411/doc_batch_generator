package core

// 数据源相关测试：isDateFormatCode / isDateNumFmtID / SheetNames /
// LoadSheetRows（用 testdata/data.xlsx 夹具做集成验证）。

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestIsDateFormatCode(t *testing.T) {
	trueCases := []string{
		"yyyy-mm-dd", "yyyy/m/d", "yyyy/mm/dd hh:mm:ss",
		"hh:mm:ss", "mm:ss", "m/d/yy",
		`yyyy"年"mm"月"dd"日"`, // 引号内的字面量不影响判定
		"[h]:mm:ss",           // [] 段被剥离，剩 h:mm:ss
		"[红色]yyyy-mm-dd",     // [] 颜色段被剥离
	}
	for _, c := range trueCases {
		if !isDateFormatCode(c) {
			t.Errorf("%q 应判定为日期格式", c)
		}
	}
	falseCases := []string{
		"General", "0.00", "#,##0", "0%", "@", "0.00E+00",
		`0"元"`, // 引号内是字面量，去掉后没有日期字母
	}
	for _, c := range falseCases {
		if isDateFormatCode(c) {
			t.Errorf("%q 不应判定为日期格式", c)
		}
	}
}

func TestIsDateNumFmtID(t *testing.T) {
	si := &styleInfo{customFmts: map[int]string{50: "yyyy-mm-dd", 51: "0.00"}}
	for _, id := range []int{14, 15, 16, 17, 18, 19, 20, 21, 22, 45, 46, 47} {
		if !si.isDateNumFmtID(id) {
			t.Errorf("内置日期格式 id %d 应判定为日期", id)
		}
	}
	if !si.isDateNumFmtID(50) || si.isDateNumFmtID(51) {
		t.Error("自定义格式应按格式码判定：50 应为日期，51 不应")
	}
	if si.isDateNumFmtID(0) {
		t.Error("id 0（General）不应判定为日期")
	}
}

func TestSheetNames(t *testing.T) {
	requireFile(t, testDataXLSX)
	names, err := SheetNames(testDataXLSX)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "Sheet1" || names[1] != "汇总" {
		t.Fatalf("sheets = %v，期望 [Sheet1 汇总]", names)
	}
	if _, err := SheetNames("不存在.xlsx"); err == nil {
		t.Error("不存在的文件应报错")
	}
}

func TestLoadSheetRowsFixture(t *testing.T) {
	requireFile(t, testDataXLSX)
	rows, err := LoadSheetRows(testDataXLSX, "Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	// 夹具共 9 行（可能含尾部空行），截断尾部空行后应为 9 行（第 9 行有数据）
	if len(rows) != 9 {
		t.Fatalf("读到 %d 行，期望 9", len(rows))
	}
	rows = TruncateTrailingEmptyRows(rows)
	if len(rows) != 9 {
		t.Fatalf("截断后 %d 行，期望 9（第 9 行有数据，不截断）", len(rows))
	}
	// 第 2 行是空白行
	if !IsEmptyRow(rows[1]) {
		t.Error("第 2 行应为空行")
	}
	// 第 3 行是标题行
	colMap, names := BuildHeaderMap(rows, 3)
	if len(names) != 5 {
		t.Fatalf("标题列 = %v，期望 5 个", names)
	}
	for _, h := range []string{"项目名称", "类型", "日期", "编号", "金额"} {
		if _, ok := colMap[h]; !ok {
			t.Errorf("缺少标题列 %q", h)
		}
	}
	// 第 4 行：项目A 开工，日期为日期类型
	r4 := rows[3]
	if r4[0].String() != "项目A" || r4[1].String() != "开工" {
		t.Fatalf("第 4 行 = %s/%s", r4[0].String(), r4[1].String())
	}
	if !r4[2].IsTime || r4[2].String() != "2026/07/05" {
		t.Fatalf("第 4 行日期 = %v，期望 2026/07/05", r4[2])
	}
	// 数值渲染：整数去小数点、小数保留
	if r4[4].String() != "123.45" {
		t.Errorf("第 4 行金额 = %q", r4[4].String())
	}
	if rows[4][3].String() != "1002" {
		t.Errorf("第 5 行编号 = %q", rows[4][3].String())
	}
	if rows[5][4].String() != "0.5" {
		t.Errorf("第 6 行金额 = %q", rows[5][4].String())
	}
	// 第 7 行项目C 无日期
	if rows[6][2].IsTime || rows[6][2].String() != "" {
		t.Errorf("第 7 行日期应为空，实际 IsTime=%v String=%q", rows[6][2].IsTime, rows[6][2].String())
	}
	// 不存在的 Sheet 报错
	if _, err := LoadSheetRows(testDataXLSX, "不存在"); err == nil {
		t.Error("不存在的 Sheet 应报错")
	}
}

// TestLoadSheetRowsTextDates 验证文本形式的日期在读入时被识别为日期值，
// 同时编号、年月等数字串不会被误判为日期。
func TestLoadSheetRowsTextDates(t *testing.T) {
	f := excelize.NewFile()
	const sh = "Sheet1"
	f.SetCellStr(sh, "A1", "2026-09-03")     // 文本日期（横杠）
	f.SetCellStr(sh, "A2", "2026/09/03")     // 文本日期（斜杠）
	f.SetCellStr(sh, "A3", "2026年09月03日")   // 文本日期（中文）
	f.SetCellStr(sh, "A4", "2026-09-03 12:30:45") // 文本日期时间
	f.SetCellStr(sh, "A5", "20260903")       // 数字串：不应误判为日期
	f.SetCellStr(sh, "A6", "2026-09")        // 年月：保持文本
	f.SetCellStr(sh, "A7", "abc")            // 普通文本
	if err := f.SetSheetDimension(sh, "A1:A7"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "textdate.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	rows, err := LoadSheetRows(path, sh)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		idx    int
		isTime bool
		want   string
	}{
		{0, true, "2026/09/03"},
		{1, true, "2026/09/03"},
		{2, true, "2026/09/03"},
		{3, true, "2026/09/03 12:30:45"},
		{4, false, "20260903"},
		{5, false, "2026-09"},
		{6, false, "abc"},
	}
	for _, c := range cases {
		v := rows[c.idx][0]
		if v.IsTime != c.isTime || v.String() != c.want {
			t.Errorf("第 %d 个单元格：IsTime=%v String=%q，期望 IsTime=%v String=%q",
				c.idx+1, v.IsTime, v.String(), c.isTime, c.want)
		}
	}
}