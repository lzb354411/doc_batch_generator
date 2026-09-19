package ui

// 「5 输出设置」页：生成文件存放位置、是否按命名项新建子文件夹。

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// pageOutput 构建「5 输出设置」区块。
func (ui *App) pageOutput() fyne.CanvasObject {
	ui.outputEntry = widget.NewEntry()
	ui.outputEntry.Disable()
	ui.outputEntry.SetText(ui.cfg.OutputFolder)

	choose := widget.NewButton("选择输出文件夹...", func() {
		go func() {
			path, ok := pickDir("选择输出文件夹", ui.outputEntry.Text)
			fyne.Do(func() {
				if !ok {
					return
				}
				ui.cfg.OutputFolder = path
				ui.outputEntry.SetText(path)
				ui.setStatus("输出文件夹：" + path)
			})
		}()
	})

	ui.subfolderCheck = widget.NewCheck("按命名项新建子文件夹", func(on bool) {
		if ui.suppress > 0 {
			return
		}
		ui.cfg.CreateSubfolder = on
		ui.updateNamingPreview()
	})
	ui.subfolderCheck.SetChecked(ui.cfg.CreateSubfolder)

	hint := themedLabel("勾选后：以「列值」类命名项叠加作为子文件夹名（「模板名」项不参与），"+
		"同一数据行的文件生成到同一子文件夹。", theme.ColorNamePlaceHolder, false)

	return sectionCard("5 · 输出设置", vbox(cardGap,
		container.NewBorder(nil, nil, nil, choose, ui.outputEntry),
		ui.subfolderCheck,
		hint,
	))
}
