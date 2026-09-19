package core

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// ---------- docx 占位符替换（对 word/document.xml 做字节级修改） ----------
//
// 语义与 Python 版（python-docx）一致：
//   - 以段落（w:p）为单位，把段内全部文本 run（w:t）的内容合并后做占位符替换，
//     结果写回首个 w:t、清空其余 w:t（即保留首 run 的字符格式）；
//   - 表格单元格内的段落同样处理；
//   - 未匹配到的占位符原样保留。
// 实现为字节级替换：除被改写的 w:t 文本外，文档其余字节
// （属性、命名空间、其他 zip 部件）完全原样保留，不经过 XML 重新编码。

// textSpan 记录一个 <w:t>…</w:t> 中文本内容的字节区间 [start, end)。
type textSpan struct{ start, end int }

// findTagEnd 从标签起始 '<' 处向后查找 '>'（引号感知，属性值中的 '>' 不会误判）。
func findTagEnd(data []byte, from int) int {
	var inQuote byte
	for i := from; i < len(data); i++ {
		c := data[i]
		if inQuote != 0 {
			if c == inQuote {
				inQuote = 0
			}
		} else if c == '"' || c == '\'' {
			inQuote = c
		} else if c == '>' {
			return i
		}
	}
	return -1
}

// scanNameEnd 返回标签名的结束位置（遇到空白、'>'、'/' 即止）。
func scanNameEnd(data []byte, from int) int {
	for i := from; i < len(data); i++ {
		switch data[i] {
		case ' ', '>', '/', '\t', '\r', '\n':
			return i
		}
	}
	return len(data)
}

// scanDocxXml 扫描 document.xml，返回按段落分组的 w:t 文本区间（仅含有 w:t 的段落）。
// 段落可嵌套（如文本框内段落），w:t 归属最内层段落。
func scanDocxXml(data []byte) ([][]textSpan, error) {
	var paras [][]textSpan
	var stack [][]textSpan
	i, n := 0, len(data)
	for i < n {
		lt := bytes.IndexByte(data[i:], '<')
		if lt < 0 {
			break
		}
		p := i + lt
		gt := findTagEnd(data, p)
		if gt < 0 {
			return nil, errors.New("标签未闭合")
		}
		ns := p + 1
		if ns < n && data[ns] == '/' {
			// 结束标签
			ne := scanNameEnd(data, ns+1)
			name := string(data[ns+1 : ne])
			if name == "w:p" && len(stack) > 0 {
				spans := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if len(spans) > 0 {
					paras = append(paras, spans)
				}
			}
			i = gt + 1
			continue
		}
		if ns < n && (data[ns] == '?' || data[ns] == '!') {
			// <?xml ...?>、<!-- 注释 --> 等声明直接跳过
			i = gt + 1
			continue
		}
		// 开始标签
		ne := scanNameEnd(data, ns)
		name := string(data[ns:ne])
		selfClosing := gt > p && data[gt-1] == '/'
		switch {
		case name == "w:p":
			stack = append(stack, nil)
			i = gt + 1
		case name == "w:t" && !selfClosing:
			textStart := gt + 1
			next := bytes.IndexByte(data[textStart:], '<')
			if next < 0 {
				return nil, errors.New("w:t 未闭合")
			}
			span := textSpan{textStart, textStart + next}
			if len(stack) > 0 {
				top := len(stack) - 1
				stack[top] = append(stack[top], span)
			}
			cgt := findTagEnd(data, textStart+next)
			if cgt < 0 {
				return nil, errors.New("w:t 未闭合")
			}
			i = cgt + 1
		default:
			i = gt + 1
		}
	}
	return paras, nil
}

// xmlEscape 转义 XML 文本节点中的特殊字符。
func xmlEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// parseNumericRef 解析数字字符引用的值部分（十进制或 x 十六进制）。
func parseNumericRef(s string) (rune, bool) {
	if s == "" {
		return 0, false
	}
	base := 10
	if s[0] == 'x' || s[0] == 'X' {
		base = 16
		s = s[1:]
	}
	v, err := strconv.ParseUint(s, base, 32)
	if err != nil {
		return 0, false
	}
	r := rune(v)
	if r == 0 || (r >= 0xD800 && r <= 0xDFFF) {
		return 0, false
	}
	return r, true
}

// xmlUnescape 还原 XML 文本节点中的实体引用。
func xmlUnescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := strings.IndexByte(s[i:], ';')
		if j < 0 || j > 12 {
			b.WriteByte(s[i])
			i++
			continue
		}
		entity := s[i+1 : i+j]
		orig := s[i : i+j+1] // 含 '&' 与 ';' 的原文
		switch entity {
		case "amp":
			b.WriteByte('&')
		case "lt":
			b.WriteByte('<')
		case "gt":
			b.WriteByte('>')
		case "quot":
			b.WriteByte('"')
		case "apos":
			b.WriteByte('\'')
		default:
			if len(entity) > 1 && entity[0] == '#' {
				if r, valid := parseNumericRef(entity[1:]); valid {
					b.WriteRune(r)
				} else {
					b.WriteString(orig) // 无效数字引用，原样保留
				}
			} else {
				b.WriteString(orig) // 未知实体，原样保留
			}
		}
		i += j + 1
	}
	return b.String()
}

// rewriteDocxXml 对扫描出的段落文本执行占位符替换，返回新 document.xml 与未匹配占位符。
func rewriteDocxXml(data []byte, paras [][]textSpan, fields map[string]CellValue) ([]byte, []string) {
	type spanEdit struct {
		start, end int
		text       string
	}
	var edits []spanEdit
	var unmatchedAll []string
	for _, spans := range paras {
		var jb strings.Builder
		for _, sp := range spans {
			jb.WriteString(xmlUnescape(string(data[sp.start:sp.end])))
		}
		full := jb.String()
		if !strings.Contains(full, "【") {
			continue
		}
		newText, unmatched := RenderText(full, fields)
		unmatchedAll = append(unmatchedAll, unmatched...)
		if newText == full {
			// 无变化（含全部未匹配）：保持原样，避免不必要的 run 合并
			continue
		}
		for si, sp := range spans {
			if si == 0 {
				edits = append(edits, spanEdit{sp.start, sp.end, xmlEscape(newText)})
			} else {
				edits = append(edits, spanEdit{sp.start, sp.end, ""})
			}
		}
	}
	if len(edits) == 0 {
		return data, unmatchedAll
	}
	sort.Slice(edits, func(a, b int) bool { return edits[a].start < edits[b].start })
	var out bytes.Buffer
	out.Grow(len(data))
	prev := 0
	for _, e := range edits {
		out.Write(data[prev:e.start])
		out.WriteString(e.text)
		prev = e.end
	}
	out.Write(data[prev:])
	return out.Bytes(), unmatchedAll
}

// readDocxEntry 读取 docx（zip）中指定条目的内容。
func readDocxEntry(path, entry string) ([]byte, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != entry {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, errors.New("docx 中找不到 " + entry)
}

// rewriteZipEntry 复制整个 zip 并替换其中一个条目的内容，
// 其余条目按原始压缩数据原样复制（保证未被修改的部件逐字节不变）。
func rewriteZipEntry(srcPath, dstPath, entry string, newContent []byte) error {
	zr, err := zip.OpenReader(srcPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	for _, f := range zr.File {
		if f.Name == entry {
			hdr := f.FileHeader
			hdr.Method = zip.Deflate
			w, err := zw.CreateHeader(&hdr)
			if err != nil {
				return err
			}
			if _, err := w.Write(newContent); err != nil {
				return err
			}
			continue
		}
		rc, err := f.OpenRaw()
		if err != nil {
			return err
		}
		w, err := zw.CreateRaw(&f.FileHeader)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, rc); err != nil {
			return err
		}
	}
	return zw.Close()
}

// ReplaceDocxPlaceholders 将 docx 模板中的占位符替换为字段值并另存为新文件。
// 返回未匹配占位符列表。对应 Python 版 _generate_docx。
func ReplaceDocxPlaceholders(templatePath, outputPath string, fields map[string]CellValue) ([]string, error) {
	docBytes, err := readDocxEntry(templatePath, "word/document.xml")
	if err != nil {
		return nil, errors.New("加载模板失败：" + err.Error())
	}
	paras, err := scanDocxXml(docBytes)
	if err != nil {
		return nil, errors.New("加载模板失败：" + err.Error())
	}
	newDoc, unmatched := rewriteDocxXml(docBytes, paras, fields)
	if err := rewriteZipEntry(templatePath, outputPath, "word/document.xml", newDoc); err != nil {
		return nil, errors.New("保存失败：" + err.Error())
	}
	return unmatched, nil
}

// DocxPlaceholders 扫描 docx 模板正文中的全部占位符名称（去重、按出现顺序）。
// 用于生成前的「占位符校验」：对比模板占位符与 Excel 列名，列出缺失项。
func DocxPlaceholders(templatePath string) ([]string, error) {
	docBytes, err := readDocxEntry(templatePath, "word/document.xml")
	if err != nil {
		return nil, err
	}
	paras, err := scanDocxXml(docBytes)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var names []string
	for _, spans := range paras {
		var jb strings.Builder
		for _, sp := range spans {
			jb.WriteString(xmlUnescape(string(docBytes[sp.start:sp.end])))
		}
		collectPlaceholderNames(jb.String(), seen, &names)
	}
	return names, nil
}
