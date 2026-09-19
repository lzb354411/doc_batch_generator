package core

// 本文件为 core 包的测试提供共享辅助函数。
// `go test` 运行时工作目录是包目录（docgen/internal/core），
// 因此测试夹具的路径用相对路径 ../../testdata/ 引用。

import (
	"os"
	"testing"
	"time"
)

const (
	// testDataXLSX 测试数据表：标题在第 3 行，共 4 个项目（见 cmd/mkfixtures）。
	testDataXLSX = "../../testdata/data.xlsx"
	// testTplDir 测试模板目录：5 个模板（docx/xlsx/csv/txt/GBK txt）。
	testTplDir = "../../testdata/templates"
)

// cv 构造一个纯文本单元格值。
func cv(s string) CellValue { return CellValue{Str: s} }

// num 构造一个数值单元格值。
func num(f float64) CellValue { return CellValue{Num: f, IsNum: true} }

// tm 从 "2006-01-02" 格式字符串构造一个日期单元格值。
func tm(s string) CellValue {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return CellValue{T: t, IsTime: true}
}

// tmd 从 "2006-01-02 15:04:05" 格式字符串构造一个含时间的日期单元格值。
func tmd(s string) CellValue {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		panic(err)
	}
	return CellValue{T: t, IsTime: true}
}

// requireFile 断言测试夹具存在；缺失时直接失败并提示先生成夹具。
func requireFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("缺少测试夹具 %s：请先在 docgen 目录运行 go run ./cmd/mkfixtures 生成", path)
	}
}

// strPtr 返回字符串指针（构造 excelize 样式用）。
func strPtr(s string) *string { return &s }