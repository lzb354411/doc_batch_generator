package ui

// 「模板分组管理」对话框：管理全局模板分组（cfg.Groups，分组名 → 模板文件名列表）。
// 支持新建 / 重命名 / 删除分组、编辑分组成员（从文件夹中勾选模板文件）。
// 分组变化后同步清理各规则引用中的已删除 / 重命名分组名，并刷新模板页的分组勾选列表。
// 分组是全局的；各规则在模板页的「按模板分组」模式下选择要使用的分组。

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// showGroupManage 弹出模板分组管理对话框。
// refresh 在每次分组变化后调用：刷新对话框内的分组列表，并同步模板页的分组勾选列表。
func (ui *App) showGroupManage() {
	box := container.NewVBox()
	refresh := func() {
		ui.rebuildGroupList(box)
		ui.rebuildTplChecks()
	}
	refresh()

	newBtn := widget.NewButton("新建分组", func() { ui.newGroupDialog(refresh) })
	scroll := container.NewVScroll(box)
	scroll.SetMinSize(fyne.NewSize(520, 360))
	content := container.NewBorder(container.NewHBox(newBtn), nil, nil, nil, scroll)
	dialog.NewCustom("模板分组管理", "关闭", content, ui.win).Show()
}

// rebuildGroupList 依据 cfg.Groups 重建分组列表行。
func (ui *App) rebuildGroupList(box *fyne.Container) {
	ui.suppress++
	defer func() { ui.suppress-- }()

	objs := make([]fyne.CanvasObject, 0, len(ui.cfg.Groups)+1)
	if len(ui.cfg.Groups) == 0 {
		objs = append(objs, widget.NewLabel("（尚未创建分组）"))
	}
	for _, g := range ui.sortedGroupNames() {
		g := g
		label := widget.NewLabel(fmt.Sprintf("%s（%d 个模板）", g, len(ui.cfg.Groups[g])))
		edit := widget.NewButton("编辑成员", func() { ui.editGroupMembers(g, nil) })
		rename := widget.NewButton("重命名", func() { ui.renameGroupDialog(g, nil) })
		del := widget.NewButton("删除", func() { ui.deleteGroupDialog(g, nil) })
		for _, b := range []*widget.Button{edit, rename, del} {
			b.Importance = widget.LowImportance
		}
		row := container.NewBorder(nil, nil, label, container.NewHBox(edit, rename, del))
		objs = append(objs, row)
	}
	box.Objects = objs
	box.Refresh()
}

// newGroupDialog 新建分组（名称去空格；重名时提示）。创建后直接进入成员编辑。
func (ui *App) newGroupDialog(refresh func()) {
	nameEntry := widget.NewEntry()
	nameEntry.PlaceHolder = "输入分组名称（如：合同、通知）"
	content := container.NewVBox(widget.NewLabel("新建分组："), nameEntry)
	d := dialog.NewCustomConfirm("新建分组", "创建", "取消", content, func(ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSpace(nameEntry.Text)
		if name == "" {
			ui.setStatus("分组名称不能为空")
			return
		}
		if _, exists := ui.cfg.Groups[name]; exists {
			dialog.ShowInformation("提示", "分组「"+name+"」已存在", ui.win)
			return
		}
		ui.cfg.Groups[name] = []string{}
		if refresh != nil {
			refresh()
		}
		ui.editGroupMembers(name, refresh) // 新建后直接添加成员
	}, ui.win)
	d.Resize(fyne.NewSize(380, 160))
	d.Show()
}

// renameGroupDialog 重命名分组，并同步更新所有规则中对该分组的引用。
func (ui *App) renameGroupDialog(g string, refresh func()) {
	nameEntry := widget.NewEntry()
	nameEntry.SetText(g)
	content := container.NewVBox(widget.NewLabel("重命名分组："), nameEntry)
	d := dialog.NewCustomConfirm("重命名分组", "确定", "取消", content, func(ok bool) {
		if !ok {
			return
		}
		newName := strings.TrimSpace(nameEntry.Text)
		if newName == "" || newName == g {
			return
		}
		if _, exists := ui.cfg.Groups[newName]; exists {
			dialog.ShowInformation("提示", "分组「"+newName+"」已存在", ui.win)
			return
		}
		ui.cfg.Groups[newName] = ui.cfg.Groups[g]
		delete(ui.cfg.Groups, g)
		for i := range ui.cfg.ContentRules {
			ui.renameGroupRef(&ui.cfg.ContentRules[i].SelectedGroups, g, newName)
		}
		for i := range ui.cfg.NumberRules {
			ui.renameGroupRef(&ui.cfg.NumberRules[i].SelectedGroups, g, newName)
		}
		if refresh != nil {
			refresh()
		}
	}, ui.win)
	d.Resize(fyne.NewSize(380, 160))
	d.Show()
}

// deleteGroupDialog 删除分组，并从所有规则的分组引用中移除该分组名。
func (ui *App) deleteGroupDialog(g string, refresh func()) {
	msg := widget.NewLabel("确定删除分组「" + g + "」吗？\n删除后引用该分组的规则将不再包含这些模板。")
	msg.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomConfirm("删除分组", "删除", "取消", msg, func(ok bool) {
		if !ok {
			return
		}
		delete(ui.cfg.Groups, g)
		for i := range ui.cfg.ContentRules {
			ui.removeGroupRef(&ui.cfg.ContentRules[i].SelectedGroups, g)
		}
		for i := range ui.cfg.NumberRules {
			ui.removeGroupRef(&ui.cfg.NumberRules[i].SelectedGroups, g)
		}
		if refresh != nil {
			refresh()
		}
	}, ui.win)
	d.Resize(fyne.NewSize(380, 160))
	d.Show()
}

// editGroupMembers 编辑分组 g 的成员：选择文件夹后从其中勾选模板文件。
// refresh 在对话框关闭时调用（用于刷新上层分组管理列表与模板页）。
func (ui *App) editGroupMembers(g string, refresh func()) {
	folderEntry := widget.NewEntry()
	folderEntry.PlaceHolder = "选择模板文件夹，列出可勾选的模板文件"
	choose := widget.NewButton("选择文件夹...", func() {
		go func() {
			path, ok := pickDir("选择模板文件夹", folderEntry.Text)
			fyne.Do(func() {
				if !ok {
					return
				}
				folderEntry.SetText(path) // SetText 触发 OnChanged → 自动重建勾选列表
			})
		}()
	})

	checksBox := container.NewVBox()
	rebuild := func() {
		ui.suppress++
		defer func() { ui.suppress-- }()
		files := scanTemplateFiles(folderEntry.Text)
		objs := make([]fyne.CanvasObject, 0, len(files)+1)
		for _, f := range files {
			f := f
			cb := widget.NewCheck(f, func(on bool) {
				if ui.suppress > 0 {
					return
				}
				ui.setGroupMember(g, f, on)
			})
			cb.SetChecked(containsStr(ui.cfg.Groups[g], f))
			objs = append(objs, cb)
		}
		if len(files) == 0 {
			objs = append(objs, widget.NewLabel("（文件夹为空或不存在）"))
		}
		checksBox.Objects = objs
		checksBox.Refresh()
	}
	folderEntry.OnChanged = func(string) { rebuild() }

	selAll := widget.NewButton("全选", func() { setBoxChecks(checksBox, true) })
	selNone := widget.NewButton("全不选", func() { setBoxChecks(checksBox, false) })
	selInv := widget.NewButton("反选", func() {
		for _, o := range checksBox.Objects {
			if cb, ok := o.(*widget.Check); ok {
				cb.SetChecked(!cb.Checked)
			}
		}
	})

	scroll := container.NewVScroll(checksBox)
	scroll.SetMinSize(fyne.NewSize(520, 320))
	content := container.NewBorder(
		container.NewVBox(
			container.NewBorder(nil, nil, widget.NewLabel("模板文件夹："), choose, folderEntry),
			container.NewHBox(selAll, selNone, selInv),
		),
		nil, nil, nil,
		scroll,
	)
	d := dialog.NewCustom(fmt.Sprintf("编辑分组「%s」的成员", g), "完成", content, ui.win)
	d.SetOnClosed(func() {
		if refresh != nil {
			refresh()
		}
	})
	d.Show()
}

// setGroupMember 把模板文件 f 加入或移出分组 g。
func (ui *App) setGroupMember(g, f string, on bool) {
	members := ui.cfg.Groups[g]
	if on {
		if !containsStr(members, f) {
			ui.cfg.Groups[g] = append(members, f)
		}
		return
	}
	out := members[:0]
	for _, n := range members {
		if n != f {
			out = append(out, n)
		}
	}
	ui.cfg.Groups[g] = out
}

// renameGroupRef 在规则的分组引用列表中把旧分组名替换为新名。
func (ui *App) renameGroupRef(list *[]string, oldName, newName string) {
	for i, n := range *list {
		if n == oldName {
			(*list)[i] = newName
		}
	}
}

// removeGroupRef 从规则的分组引用列表中删除指定分组名。
func (ui *App) removeGroupRef(list *[]string, name string) {
	out := (*list)[:0]
	for _, n := range *list {
		if n != name {
			out = append(out, n)
		}
	}
	*list = out
}

// setBoxChecks 把容器内所有 Check 控件设为指定勾选状态。
func setBoxChecks(box *fyne.Container, on bool) {
	for _, o := range box.Objects {
		if cb, ok := o.(*widget.Check); ok {
			cb.SetChecked(on)
		}
	}
}
