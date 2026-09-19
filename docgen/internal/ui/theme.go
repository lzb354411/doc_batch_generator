package ui

import (
	"image/color"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// ============ TraeWork 风格设计变量 ============
// 整体风格借鉴 TraeWork：浅灰画布 + 白色卡片 + 细边框 + 绿色主色，
// 默认按钮白底绿字，主按钮（开始生成）绿底白字，输入框聚焦绿色描边。
var (
	colCanvas      = color.NRGBA{0xF6, 0xF7, 0xF9, 0xFF} // 窗口画布（浅灰）
	colCard        = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF} // 卡片 / 输入框背景（白）
	colBorder      = color.NRGBA{0xE3, 0xE6, 0xEB, 0xFF} // 卡片细边框
	colInputBorder = color.NRGBA{0xD9, 0xDD, 0xE3, 0xFF} // 输入框描边
	colPrimary     = color.NRGBA{0x00, 0xA8, 0x70, 0xFF} // 主色（绿）
	colPrimaryTint = color.NRGBA{0xEA, 0xF7, 0xF1, 0xFF} // 主色淡底（选中 / 悬停高亮）
	colText        = color.NRGBA{0x1D, 0x21, 0x29, 0xFF} // 主文字
	colPlaceholder = color.NRGBA{0x98, 0xA1, 0xAB, 0xFF} // 占位文字
	colDisabled    = color.NRGBA{0xC2, 0xC9, 0xD2, 0xFF} // 禁用状态
	colDisabledBtn = color.NRGBA{0xF2, 0xF3, 0xF5, 0xFF} // 禁用按钮
	colError       = color.NRGBA{0xE5, 0x48, 0x4D, 0xFF} // 错误 / 失败
	colWarning     = color.NRGBA{0xEB, 0x9A, 0x00, 0xFF} // 提醒
	colScrollbar   = color.NRGBA{0xC7, 0xCD, 0xD5, 0xFF} // 滚动条
	colHeader      = color.NRGBA{0xFA, 0xFB, 0xFC, 0xFF} // 表头
	colSeparator   = color.NRGBA{0xED, 0xEF, 0xF2, 0xFF} // 分隔线
)

// ============ 中文字体 ============
// Fyne 内置字体不包含中文字形，默认主题下中文会显示为方框。
// 这里在启动时从 Windows 系统字体目录加载黑体（simhei.ttf 是单一字体的
// TrueType 文件，Fyne 可直接解析），通过自定义 Theme 覆盖默认字体。
// 找不到时退回内置主题（界面仍可用，仅中文显示异常）。

// zhFontRes 加载成功的中文系统字体资源（nil 表示未加载）。
var zhFontRes fyne.Resource

// chineseFontPaths 按优先级尝试的字体路径。
// 只放 .ttf（单一字体文件）；.ttc 字体集合 Fyne 无法直接解析，不列入。
var chineseFontPaths = []string{
	`C:\Windows\Fonts\simhei.ttf`,
}

// applyTraeTheme 应用 TraeWork 风格主题，并尽量加载系统中文字体。
// 需在创建窗口之前调用。
func applyTraeTheme(a fyne.App) {
	loadChineseFont()
	a.Settings().SetTheme(traeTheme{base: theme.DefaultTheme()})
}

// loadChineseFont 尝试加载中文字体资源到 zhFontRes。
func loadChineseFont() {
	for _, p := range chineseFontPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		zhFontRes = fyne.NewStaticResource(filepath.Base(p), data)
		return
	}
	zhFontRes = nil
}

// traeTheme 实现 TraeWork 风格主题：固定浅色配色（忽略系统深浅色），
// 未列出的颜色、图标、尺寸委托给内置默认主题。
type traeTheme struct {
	base fyne.Theme
}

// Color 返回 TraeWork 风格配色；固定使用浅色变体。
func (t traeTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return colCanvas
	case theme.ColorNameButton:
		return colCard
	case theme.ColorNameDisabledButton:
		return colDisabledBtn
	case theme.ColorNameDisabled:
		return colDisabled
	case theme.ColorNamePlaceHolder:
		return colPlaceholder
	case theme.ColorNameForeground:
		return colText
	case theme.ColorNamePrimary:
		return colPrimary
	case theme.ColorNameForegroundOnPrimary:
		return color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	case theme.ColorNameFocus:
		return colPrimary
	case theme.ColorNameHover:
		return color.NRGBA{0x1D, 0x21, 0x29, 0x08} // 半透明黑：悬停轻微变深
	case theme.ColorNamePressed:
		return color.NRGBA{0x1D, 0x21, 0x29, 0x14}
	case theme.ColorNameSelection:
		return colPrimaryTint
	case theme.ColorNameInputBackground:
		return colCard
	case theme.ColorNameInputBorder:
		return colInputBorder
	case theme.ColorNameMenuBackground:
		return colCard
	case theme.ColorNameOverlayBackground:
		return colCard
	case theme.ColorNameHeaderBackground:
		return colHeader
	case theme.ColorNameSeparator:
		return colSeparator
	case theme.ColorNameSuccess:
		return colPrimary
	case theme.ColorNameForegroundOnSuccess:
		return color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	case theme.ColorNameError:
		return colError
	case theme.ColorNameForegroundOnError:
		return color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	case theme.ColorNameWarning:
		return colWarning
	case theme.ColorNameForegroundOnWarning:
		return color.NRGBA{0x1D, 0x21, 0x29, 0xFF}
	case theme.ColorNameScrollBar:
		return colScrollbar
	case theme.ColorNameScrollBarBackground:
		return color.NRGBA{0x00, 0x00, 0x00, 0x00}
	case theme.ColorNameShadow:
		return color.NRGBA{0x1D, 0x21, 0x29, 0x33}
	case theme.ColorNameInnerWindowBorder:
		return colBorder
	case theme.ColorNameInnerWindowBorderInactive:
		return colBorder
	}
	return t.base.Color(name, variant)
}

// Size 返回 TraeWork 风格尺寸（圆角、间距），其余委托默认主题。
func (t traeTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 5
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameInputRadius:
		return 6
	case theme.SizeNameButtonRadius:
		return 6
	case theme.SizeNameSelectionRadius:
		return 5
	case theme.SizeNameDialogRadius:
		return 10
	}
	return t.base.Size(name)
}

// Icon 委托默认主题图标（保持全部内置图标风格统一）。
func (t traeTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

// Font 返回中文字体（粗体、斜体等样式也使用同一字体）。
func (t traeTheme) Font(style fyne.TextStyle) fyne.Resource {
	if zhFontRes != nil {
		return zhFontRes
	}
	return t.base.Font(style)
}