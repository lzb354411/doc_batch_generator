package ui

// Windows 原生「资源管理器」式文件/文件夹选择对话框封装。
// 替换 Fyne 内置对话框（外观、交互均与系统文件管理器一致）：
//   选择文件   → GetOpenFileName（打开文件对话框）
//   选择文件夹 → SHBrowseForFolder（浏览文件夹对话框）
//
// 使用约定：对话框在独立 goroutine 中弹出（Windows 上通用对话框自带消息循环，
// 不会阻塞 Fyne 渲染线程），拿到结果后用 fyne.Do 切回主线程更新界面控件。

import "github.com/sqweek/dialog"

// pickFile 弹出 Windows 原生「打开文件」对话框。
// exts 为去掉点的扩展名列表（如 []string{"xlsx","xlsm"}），为空时允许任意文件。
// 返回（选中路径, 是否确认）；用户取消或对话框出错时 ok=false。
func pickFile(title, initialDir string, exts []string) (string, bool) {
	b := dialog.File().Title(title)
	if len(exts) > 0 {
		b.Filter("支持的格式", exts...)
	}
	if initialDir != "" {
		b.SetStartDir(initialDir)
	}
	path, err := b.Load()
	if err != nil {
		return "", false // 包含 ErrCancelled（用户取消）与对话框异常，一律视为未选择
	}
	return path, true
}

// pickDir 弹出 Windows 原生「选择文件夹」对话框。
// 返回（选中路径, 是否确认）；用户取消或对话框出错时 ok=false。
func pickDir(title, initialDir string) (string, bool) {
	b := dialog.Directory().Title(title)
	if initialDir != "" {
		b.SetStartDir(initialDir)
	}
	path, err := b.Browse()
	if err != nil {
		return "", false
	}
	return path, true
}
