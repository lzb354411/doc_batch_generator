package core

// 占位符替换测试：RenderText / collectPlaceholderNames。
// 语法：【列名】、【列名|日期格式】；未匹配的占位符原样保留。

import (
	"testing"
)

func TestRenderText(t *testing.T) {
	fields := map[string]CellValue{
		"项目名称": cv("项目A"),
		"类型":   cv("开工"),
		"日期":   tm("2026-07-05"),
	}

	t.Run("无占位符文本原样返回", func(t *testing.T) {
		out, unmatched := RenderText("普通文本", fields)
		if out != "普通文本" || unmatched != nil {
			t.Fatalf("got %q, %v", out, unmatched)
		}
	})

	t.Run("全部匹配", func(t *testing.T) {
		out, unmatched := RenderText("项目：【项目名称】 类型：【类型】", fields)
		if out != "项目：项目A 类型：开工" {
			t.Fatalf("got %q", out)
		}
		if len(unmatched) != 0 {
			t.Fatalf("不应有未匹配项：%v", unmatched)
		}
	})

	t.Run("日期格式化", func(t *testing.T) {
		out, _ := RenderText("日期：【日期|YYYY年MM月DD日】", fields)
		if out != "日期：2026年07月05日" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("未匹配占位符原样保留并收集", func(t *testing.T) {
		out, unmatched := RenderText("【不存在的列】-【项目名称】", fields)
		if out != "【不存在的列】-项目A" {
			t.Fatalf("got %q", out)
		}
		if len(unmatched) != 1 || unmatched[0] != "不存在的列" {
			t.Fatalf("未匹配列表 = %v，期望 [不存在的列]", unmatched)
		}
	})

	t.Run("占位符内空白被去除", func(t *testing.T) {
		out, _ := RenderText("【 项目名称 】", fields)
		if out != "项目A" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("带格式的未匹配只记列名", func(t *testing.T) {
		_, unmatched := RenderText("【未知列|YYYY年MM月DD日】", fields)
		if len(unmatched) != 1 || unmatched[0] != "未知列" {
			t.Fatalf("未匹配列表 = %v，期望 [未知列]", unmatched)
		}
	})
}

func TestCollectPlaceholderNames(t *testing.T) {
	seen := map[string]bool{}
	var names []string
	collectPlaceholderNames("【项目名称】与【类型】和【项目名称|YYYY】", seen, &names)
	collectPlaceholderNames("普通文本", seen, &names)
	if len(names) != 2 || names[0] != "项目名称" || names[1] != "类型" {
		t.Fatalf("names = %v，期望 [项目名称 类型]（去重保序）", names)
	}
}