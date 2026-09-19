package ui

// 通用布局组件：TraeWork 风格的白色区块卡片、底部通栏与间距布局。
// 卡片为 1px 细边框 + 内边距 + 可选标题行（绿色竖条 + 加粗标题），
// 内容按垂直方向自然堆叠，宽度自动铺满卡片。

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const (
	cardPad  = 16 // 卡片内边距
	cardGap  = 10 // 卡片内行间距
	cardLine = 1  // 卡片描边宽度
)

// ---------- 间距布局 ----------

// vPadLayout 垂直排列子对象：整体内边距 pad，行间距 gap，宽度铺满容器。
// 行为与 fyne 的 VBoxLayout 一致（子对象按 MinSize 堆叠、横向拉伸），
// 区别是可自定义内边距与行间距。
type vPadLayout struct{ pad, gap float32 }

func (l vPadLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	innerW := size.Width - 2*l.pad
	if innerW < 0 {
		innerW = 0
	}
	y := l.pad
	first := true
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		if !first {
			y += l.gap
		}
		first = false
		m := o.MinSize()
		w := innerW
		if m.Width > w {
			w = m.Width
		}
		o.Move(fyne.NewPos(l.pad, y))
		o.Resize(fyne.NewSize(w, m.Height))
		y += m.Height
	}
}

func (l vPadLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	w, h := float32(0), float32(0)
	first := true
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		m := o.MinSize()
		if m.Width > w {
			w = m.Width
		}
		if !first {
			h += l.gap
		}
		first = false
		h += m.Height
	}
	return fyne.NewSize(w+2*l.pad, h+2*l.pad)
}

// boxLayout 简单水平/垂直排列，可指定间距（不铺满主轴的尺寸，按 MinSize）。
type boxLayout struct {
	horizontal bool
	spacing    float32
}

func (b boxLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	pos := float32(0)
	first := true
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		m := o.MinSize()
		if !first {
			pos += b.spacing
		}
		first = false
		if b.horizontal {
			o.Move(fyne.NewPos(pos, 0))
			o.Resize(fyne.NewSize(m.Width, size.Height))
			pos += m.Width
		} else {
			o.Move(fyne.NewPos(0, pos))
			o.Resize(fyne.NewSize(size.Width, m.Height))
			pos += m.Height
		}
	}
}

func (b boxLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	w, h := float32(0), float32(0)
	first := true
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		m := o.MinSize()
		if !first {
			if b.horizontal {
				w += b.spacing
			} else {
				h += b.spacing
			}
		}
		first = false
		if b.horizontal {
			w += m.Width
			if m.Height > h {
				h = m.Height
			}
		} else {
			h += m.Height
			if m.Width > w {
				w = m.Width
			}
		}
	}
	return fyne.NewSize(w, h)
}

// hbox / vbox 便捷构造（spacing 为子对象间距）。
func hbox(spacing float32, objs ...fyne.CanvasObject) *fyne.Container {
	return container.New(boxLayout{horizontal: true, spacing: spacing}, objs...)
}

func vbox(spacing float32, objs ...fyne.CanvasObject) *fyne.Container {
	return container.New(boxLayout{spacing: spacing}, objs...)
}

// themedLabel 构建带主题色文字的标签。
// Fyne v2 的 widget.Label 不支持直接指定颜色/字号，这里用 RichText
// 文本段实现：颜色取主题色名（primary/placeholder 等），可选加粗。
func themedLabel(text string, colorName fyne.ThemeColorName, bold bool) fyne.CanvasObject {
	return widget.NewRichText(&widget.TextSegment{
		Style: widget.RichTextStyle{
			ColorName: colorName,
			TextStyle: fyne.TextStyle{Bold: bold},
		},
		Text: text,
	})
}

// ---------- 白色区块卡片 ----------

// sectionCard 构建一个白色卡片区块：1px 细边框 + 内边距。
// title 非空时顶部显示「绿色竖条 + 加粗标题」，content 为卡片正文。
func sectionCard(title string, content fyne.CanvasObject) fyne.CanvasObject {
	w := &panelCard{title: title, content: content}
	w.ExtendBaseWidget(w)
	return w
}

// panelCard 白色卡片控件（内部实现）。
type panelCard struct {
	widget.BaseWidget
	title   string
	content fyne.CanvasObject
}

type panelCardRenderer struct {
	w      *panelCard
	border *canvas.Rectangle
	face   *canvas.Rectangle
	body   fyne.CanvasObject
}

func (p *panelCard) CreateRenderer() fyne.WidgetRenderer {
	var body *fyne.Container
	if p.title != "" {
		accent := canvas.NewRectangle(colPrimary)
		accent.SetMinSize(fyne.NewSize(4, 16))
		title := widget.NewRichText(&widget.TextSegment{
			Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Bold: true}},
			Text:  p.title,
		})
		body = container.New(vPadLayout{pad: cardPad, gap: cardGap},
			hbox(8, accent, title), p.content)
	} else {
		body = container.New(vPadLayout{pad: cardPad, gap: cardGap}, p.content)
	}
	return &panelCardRenderer{
		w:      p,
		border: canvas.NewRectangle(colBorder),
		face:   canvas.NewRectangle(colCard),
		body:   body,
	}
}

func (r *panelCardRenderer) Layout(size fyne.Size) {
	inner := fyne.NewSize(size.Width-2*cardLine, size.Height-2*cardLine)
	r.border.Resize(size)
	r.border.Move(fyne.NewPos(0, 0))
	r.face.Resize(inner)
	r.face.Move(fyne.NewPos(cardLine, cardLine))
	r.body.Resize(inner)
	r.body.Move(fyne.NewPos(cardLine, cardLine))
}

func (r *panelCardRenderer) MinSize() fyne.Size {
	s := r.body.MinSize()
	return fyne.NewSize(s.Width+2*cardLine, s.Height+2*cardLine)
}

func (r *panelCardRenderer) Refresh() {
	canvas.Refresh(r.w)
}

func (r *panelCardRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.border, r.face, r.body}
}

func (r *panelCardRenderer) BackgroundColor() color.Color { return color.Transparent }
func (r *panelCardRenderer) Destroy()                     {}

// ---------- 底部通栏 ----------

// footerBar 构建底部通栏：白色背景 + 顶部 1px 分隔线，用于窗口底部的状态栏。
func footerBar(content fyne.CanvasObject) fyne.CanvasObject {
	w := &panelFooter{content: content}
	w.ExtendBaseWidget(w)
	return w
}

// panelFooter 底部通栏控件（内部实现）。
type panelFooter struct {
	widget.BaseWidget
	content fyne.CanvasObject
}

type panelFooterRenderer struct {
	w    *panelFooter
	face *canvas.Rectangle
	line *canvas.Rectangle
	body fyne.CanvasObject
}

func (f *panelFooter) CreateRenderer() fyne.WidgetRenderer {
	body := container.New(vPadLayout{pad: 10, gap: 0}, f.content)
	return &panelFooterRenderer{
		w:    f,
		face: canvas.NewRectangle(colCard),
		line: canvas.NewRectangle(colBorder),
		body: body,
	}
}

func (r *panelFooterRenderer) Layout(size fyne.Size) {
	r.face.Resize(size)
	r.face.Move(fyne.NewPos(0, 0))
	r.line.Resize(fyne.NewSize(size.Width, cardLine))
	r.line.Move(fyne.NewPos(0, 0))
	r.body.Resize(size)
	r.body.Move(fyne.NewPos(0, 0))
}

func (r *panelFooterRenderer) MinSize() fyne.Size {
	s := r.body.MinSize()
	return fyne.NewSize(s.Width, s.Height+cardLine)
}

func (r *panelFooterRenderer) Refresh() {
	canvas.Refresh(r.w)
}

func (r *panelFooterRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.face, r.line, r.body}
}

func (r *panelFooterRenderer) BackgroundColor() color.Color { return color.Transparent }
func (r *panelFooterRenderer) Destroy()                     {}