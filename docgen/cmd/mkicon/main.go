// mkicon —— 用纯 Go 代码绘制应用图标（不依赖任何图像库与转换工具）。
//
//	设计：蓝色圆角渐变背景 + 白色文档页（三条内容线）+ 绿色完成对勾徽标，
//	语义为「文档批量生成」。
//
//	go run ./cmd/mkicon   生成 assets/icon.png（256）、assets/icon.ico（16~256 多尺寸）、assets/icon.svg（设计源）
//
// 说明：4 倍超采样绘制实现抗锯齿；ICO 使用 PNG 帧（Windows Vista+ 原生支持）。
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

const supersample = 4 // 以 4 倍尺寸绘制再降采样，消除锯齿

// ------------- 基本几何（有符号距离函数） -------------

type pt struct{ x, y float64 }

type roundRect struct {
	x, y, w, h, r float64
}

// sd 返回点到圆角矩形的有符号距离（<0 在内部）。
func (rr roundRect) sd(p pt) float64 {
	cx, cy := rr.x+rr.w/2, rr.y+rr.h/2
	r := rr.r
	qx := math.Abs(p.x-cx) - (rr.w/2 - r)
	qy := math.Abs(p.y-cy) - (rr.h/2 - r)
	ox, oy := math.Max(qx, 0), math.Max(qy, 0)
	return math.Min(math.Max(qx, qy), 0) + math.Hypot(ox, oy) - r
}

// segDist 返回点到线段 a-b 的距离。
func segDist(p, a, b pt) float64 {
	abx, aby := b.x-a.x, b.y-a.y
	t := ((p.x-a.x)*abx + (p.y-a.y)*aby) / (abx*abx + aby*aby)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(p.x-(a.x+abx*t), p.y-(a.y+aby*t))
}

// stroke 表示一条线段描边（宽度 w）。
type stroke struct {
	a, b    pt
	w       float64
	col     color.RGBA
	roundCap bool
}

func (s stroke) sd(p pt) float64 {
	d := segDist(p, s.a, s.b) - s.w/2
	// 圆头端点：把端点附近的边界收成圆形
	ea, eb := s.a, s.b
	da := math.Hypot(p.x-ea.x, p.y-ea.y)
	db := math.Hypot(p.x-eb.x, p.y-eb.y)
	ra := da - s.w/2
	rb := db - s.w/2
	if s.roundCap {
		if ra > d {
			d = ra
		}
		if rb > d {
			d = rb
		}
	}
	return d
}

// lerp 两色插值。
func lerpColor(c1, c2 color.RGBA, t float64) color.RGBA {
	t = math.Max(0, math.Min(1, t))
	return color.RGBA{
		uint8(float64(c1.R) + (float64(c2.R)-float64(c1.R))*t),
		uint8(float64(c1.G) + (float64(c2.G)-float64(c1.G))*t),
		uint8(float64(c1.B) + (float64(c2.B)-float64(c1.B))*t),
		255,
	}
}

// blendSrcOver 把半透明前景合成到背景上。
func blendSrcOver(bg, fg color.RGBA) color.RGBA {
	a := float64(fg.A) / 255
	r := float64(bg.R)*(1-a) + float64(fg.R)*a
	g := float64(bg.G)*(1-a) + float64(fg.G)*a
	b := float64(bg.B)*(1-a) + float64(fg.B)*a
	al := float64(bg.A)*(1-a) + float64(fg.A)
	return color.RGBA{uint8(r), uint8(g), uint8(b), uint8(al)}
}

// ------------- 图标渲染 -------------

func drawIcon(size int) *image.RGBA {
	// 4 倍超采样画布
	big := image.NewRGBA(image.Rect(0, 0, size*supersample, size*supersample))
	S := float64(size) / 256 * supersample // 把 256 坐标系换算到超采样画布

	// 从 256 坐标系构造元素
	bg := roundRect{40, 40, 176, 176, 48}
	docShadow := roundRect{68, 58 + 7, 88, 140, 14}
	doc := roundRect{68, 58, 88, 140, 14}
	line1 := roundRect{84, 86, 68, 9, 5}
	line2 := roundRect{84, 104, 56, 9, 5}
	line3 := roundRect{84, 122, 62, 9, 5}
	badge := pt{136, 164}
	check := []stroke{
		{pt{127, 165}, pt{133, 171}, 6.5, color.RGBA{255, 255, 255, 255}, true},
		{pt{133, 171}, pt{146, 157}, 6.5, color.RGBA{255, 255, 255, 255}, true},
	}

	bgTop := color.RGBA{47, 107, 255, 255}
	bgBot := color.RGBA{30, 80, 224, 255}

	for y := 0; y < big.Rect.Dy(); y++ {
		for x := 0; x < big.Rect.Dx(); x++ {
			p := pt{float64(x) / S, float64(y) / S}
			var col color.RGBA
			// 1. 渐变背景
			t := (p.y - 40) / 176
			col = lerpColor(bgTop, bgBot, t)
			dBack := bg.sd(p)
			if dBack >= 0 {
				col.A = 0
			}
			// 2. 文档投影（半透明黑，偏移 +7px）
			dSh := docShadow.sd(p)
			if dSh < 0 && col.A > 0 {
				col = blendSrcOver(col, color.RGBA{10, 20, 60, 72})
			}
			// 3. 白色文档
			dDoc := doc.sd(p)
			if dDoc < 0 {
				col = color.RGBA{255, 255, 255, 255}
			}
			// 4. 内容线（浅蓝灰）
			for _, ln := range []roundRect{line1, line2, line3} {
				if ln.sd(p) < 0 {
					col = color.RGBA{178, 192, 240, 255}
				}
			}
			// 5. 绿色徽标 + 白色对勾
			dBadge := math.Hypot(p.x-badge.x, p.y-badge.y) - 18
			if dBadge < 0 {
				t2 := (p.y - (badge.y - 18)) / 36
				col = lerpColor(color.RGBA{34, 197, 94, 255}, color.RGBA{21, 128, 61, 255}, t2)
			}
			for _, st := range check {
				if st.sd(p) < 0 {
					col = st.col
				}
			}
			if col.A == 0 {
				continue
			}
			big.SetRGBA(x, y, col)
		}
	}

	// 降采样到目标尺寸
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a uint32
			n := 0
			for sy := 0; sy < supersample; sy++ {
				for sx := 0; sx < supersample; sx++ {
					c := big.RGBAAt(x*supersample+sx, y*supersample+sy)
					r += uint32(c.R) * uint32(c.A)
					g += uint32(c.G) * uint32(c.A)
					b += uint32(c.B) * uint32(c.A)
					a += uint32(c.A)
					n++
				}
			}
			if a == 0 {
				continue
			}
			out.SetRGBA(x, y, color.RGBA{
				uint8(r / a), uint8(g / a), uint8(b / a), uint8(a / uint32(n)),
			})
		}
	}
	return out
}

// ------------- ICO 输出（PNG 帧打包） -------------

func icoHeaderEntries(n int) []byte {
	b := make([]byte, 6)
	// ICONDIR
	b[0], b[1] = 0, 0 // reserved
	b[2], b[3] = 1, 0 // type = icon
	b[4] = byte(n)
	b[5] = 0
	return b
}

func writeICO(path string, imgBySize map[int]*image.RGBA, sizes []int) error {
	var buf []byte
	buf = append(buf, icoHeaderEntries(len(sizes))...)
	offset := 6 + 16*len(sizes)
	datas := make([][]byte, len(sizes))
	for i, sz := range sizes {
		w := &writer{}
		if err := png.Encode(w, imgBySize[sz]); err != nil {
			return err
		}
		pngBuf := w.buf
		datas[i] = pngBuf
		dim := byte(sz & 0xFF)
		if sz == 256 {
			dim = 0
		}
		// ICONDIRENTRY
		ent := make([]byte, 16)
		ent[0] = dim
		ent[1] = dim
		ent[2], ent[3] = 0, 0
		ent[4], ent[5] = 1, 0 // planes
		ent[6], ent[7] = 32, 0 // bpp
		putU32(ent[8:], uint32(len(pngBuf)))
		putU32(ent[12:], uint32(offset))
		buf = append(buf, ent...)
		offset += len(pngBuf)
	}
	for i := range datas {
		buf = append(buf, datas[i]...)
	}
	return os.WriteFile(path, buf, 0o644)
}

type writer struct{ buf []byte }

func (w *writer) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func putU32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

// ------------- 经典 ICO（BMP 帧，供 Inno Setup 安装器图标使用） -------------

// writeClassicICO 生成仅含经典 BITMAP 帧的 .ico：Inno Setup 的 SetupIconFile
// 必须用 BMP 帧，不能用纯 PNG 帧（否则 UpdateResource 报错 87）。
func writeClassicICO(path string, bySize map[int]*image.RGBA, sizes []int) error {
	frames := make([][]byte, len(sizes))
	for i, sz := range sizes {
		frames[i] = bmpIcoFrame(sz, bySize[sz])
	}
	var buf []byte
	buf = append(buf, icoHeaderEntries(len(sizes))...)
	offset := 6 + 16*len(sizes)
	for i, sz := range sizes {
		dim := byte(sz & 0xFF)
		ent := make([]byte, 16)
		ent[0], ent[1] = dim, dim
		ent[2], ent[3] = 0, 0
		ent[4], ent[5] = 1, 0
		ent[6], ent[7] = 32, 0
		putU32(ent[8:], uint32(len(frames[i])))
		putU32(ent[12:], uint32(offset))
		buf = append(buf, ent...)
		offset += len(frames[i])
	}
	for i := range frames {
		buf = append(buf, frames[i]...)
	}
	return os.WriteFile(path, buf, 0o644)
}

// bmpIcoFrame 把 RGBA 图编码为 ICO 里的 BITMAP 帧（32bpp，含 AND 掩码）。
func bmpIcoFrame(sz int, img *image.RGBA) []byte {
	const biSize = 40
	stride := sz * 4
	andStride := ((sz + 31) / 32) * 4
	dataLen := biSize + stride*sz + andStride*sz
	frame := make([]byte, dataLen)
	// BITMAPINFOHEADER
	bi := frame[:biSize]
	putU32(bi[0:], biSize)
	putI32(bi[4:], int32(sz))
	putI32(bi[8:], int32(sz*2)) // 高度 ×2（XOR + AND）
	putU16(bi[12:], 1)
	putU16(bi[14:], 32)
	putU32(bi[16:], 0) // BI_RGB
	putU32(bi[20:], uint32(stride*sz))
	// 像素（BGRA，自上而下）
	pix := frame[biSize : biSize+stride*sz]
	for y := 0; y < sz; y++ {
		for x := 0; x < sz; x++ {
			c := img.RGBAAt(x, y)
			off := y*stride + x*4
			pix[off] = c.B
			pix[off+1] = c.G
			pix[off+2] = c.R
			pix[off+3] = c.A
		}
	}
	// AND 掩码全 0（已用 alpha，无需 1bpp 掩码）
	return frame
}

func putI32(b []byte, v int32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func putU16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

// ------------- SVG 设计源 -------------

const svgDoc = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 256 256">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#2F6BFF"/>
      <stop offset="1" stop-color="#1E50E0"/>
    </linearGradient>
    <linearGradient id="badge" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#22C55E"/>
      <stop offset="1" stop-color="#15803D"/>
    </linearGradient>
  </defs>
  <rect x="40" y="40" width="176" height="176" rx="48" fill="url(#bg)"/>
  <rect x="68" y="65" width="88" height="140" rx="14" fill="rgba(10,20,60,0.28)"/>
  <rect x="68" y="58" width="88" height="140" rx="14" fill="#FFFFFF"/>
  <rect x="84" y="86" width="68" height="9" rx="4.5" fill="#B2C0F0"/>
  <rect x="84" y="104" width="56" height="9" rx="4.5" fill="#B2C0F0"/>
  <rect x="84" y="122" width="62" height="9" rx="4.5" fill="#B2C0F0"/>
  <circle cx="136" cy="164" r="18" fill="url(#badge)"/>
  <polyline points="127,165 133,171 146,157" fill="none" stroke="#FFFFFF" stroke-width="6.5" stroke-linecap="round" stroke-linejoin="round"/>
</svg>
`

func main() {
	assetsDir := filepath.Join("assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		fmt.Println("错误：", err)
		os.Exit(1)
	}
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	bySize := map[int]*image.RGBA{}
	for _, s := range sizes {
		bySize[s] = drawIcon(s)
	}
	// 256 主图
	if err := savePNG(filepath.Join(assetsDir, "icon.png"), bySize[256]); err != nil {
		fmt.Println("错误：", err)
		os.Exit(1)
	}
	if err := writeICO(filepath.Join(assetsDir, "icon.ico"), bySize, sizes); err != nil {
		fmt.Println("错误：", err)
		os.Exit(1)
	}
	// 经典 BMP 帧图标：Inno Setup 安装器的 SetupIconFile 专用
	if err := writeClassicICO(filepath.Join(assetsDir, "setup_icon.ico"), bySize, []int{16, 32, 48}); err != nil {
		fmt.Println("错误：", err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "icon.svg"), []byte(svgDoc), 0o644); err != nil {
		fmt.Println("错误：", err)
		os.Exit(1)
	}
	fmt.Printf("已生成：%s、%s、%s、%s（尺寸 %v）\n",
		filepath.Join(assetsDir, "icon.png"), filepath.Join(assetsDir, "icon.ico"),
		filepath.Join(assetsDir, "setup_icon.ico"), filepath.Join(assetsDir, "icon.svg"), sizes)
}

func savePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}