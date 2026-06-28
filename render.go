package main

import (
	"fmt"
	"os"
	"strings"
)

// colors は端末が TTY のときだけ ANSI カラーを返す。
type colors struct {
	reset, dim, bold                string
	todo, prog, block, review, done string
}

func newColors() colors {
	if !isTTY(os.Stdout) {
		return colors{}
	}
	return colors{
		reset:  "\033[0m",
		dim:    "\033[2m",
		bold:   "\033[1m",
		todo:   "\033[37m",
		prog:   "\033[36m",
		block:  "\033[31m",
		review: "\033[35m",
		done:   "\033[32m",
	}
}

func (c colors) status(s string) string {
	switch s {
	case "in-progress":
		return c.prog
	case "blocked":
		return c.block
	case "review":
		return c.review
	case "done":
		return c.done
	default:
		return c.todo
	}
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// cell は 1 マス。color はセル内容に付ける前置 ANSI (幅計算には含めない)。
type cell struct {
	text  string
	color string
}

type table struct {
	headers []string
	rows    [][]cell
}

func newTable(headers ...string) *table {
	return &table{headers: headers}
}

func (t *table) add(cells ...cell) {
	t.rows = append(t.rows, cells)
}

func (t *table) render(w *os.File, c colors) {
	n := len(t.headers)
	widths := make([]int, n)
	for i, h := range t.headers {
		widths[i] = dispWidth(h)
	}
	for _, row := range t.rows {
		for i := 0; i < n && i < len(row); i++ {
			if dw := dispWidth(row[i].text); dw > widths[i] {
				widths[i] = dw
			}
		}
	}

	const gap = 2
	var b strings.Builder
	// ヘッダ
	for i, h := range t.headers {
		writePadded(&b, h, c.bold, c.reset, widths[i], i == n-1, gap)
	}
	b.WriteByte('\n')
	// 行
	for _, row := range t.rows {
		for i := 0; i < n; i++ {
			cl := cell{}
			if i < len(row) {
				cl = row[i]
			}
			writePadded(&b, cl.text, cl.color, c.reset, widths[i], i == n-1, gap)
		}
		b.WriteByte('\n')
	}
	fmt.Fprint(w, b.String())
}

// writePadded は color+text+reset を出力し、表示幅に合わせて空白で右詰めパディングする。
func writePadded(b *strings.Builder, text, color, reset string, width int, last bool, gap int) {
	if color != "" {
		b.WriteString(color)
		b.WriteString(text)
		b.WriteString(reset)
	} else {
		b.WriteString(text)
	}
	if last {
		return
	}
	if pad := width - dispWidth(text) + gap; pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
	}
}

// dispWidth は端末上の表示幅を返す。CJK / 全角は 2 と数える。
func dispWidth(s string) int {
	w := 0
	for _, r := range s {
		if isWide(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func isWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0x303E, // CJK Radicals .. Kangxi
		r >= 0x3041 && r <= 0x33FF, // Hiragana .. CJK symbols
		r >= 0x3400 && r <= 0x4DBF, // CJK Ext A
		r >= 0x4E00 && r <= 0x9FFF, // CJK Unified
		r >= 0xA000 && r <= 0xA4CF, // Yi
		r >= 0xAC00 && r <= 0xD7A3, // Hangul Syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK Compatibility
		r >= 0xFE30 && r <= 0xFE4F, // CJK Compatibility Forms
		r >= 0xFF00 && r <= 0xFF60, // Fullwidth Forms
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x20000 && r <= 0x3FFFD: // CJK Ext B+
		return true
	}
	return false
}
