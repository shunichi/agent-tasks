package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// colors は色を出すと決まったときだけ ANSI カラーを返す (出さないときは全フィールド空)。
type colors struct {
	reset, dim, bold                      string
	todo, prog, block, review, done, warn string
}

// colorMode は --color フラグの値 (always|auto|never)。main が解決して設定する。
// 既定 "auto" は従来どおり stdout が TTY のときだけ色を出す。
var colorMode = "auto"

// colorEnabled は色を出すかを決める。優先順位:
//  1. --color フラグ (always / never) が最優先。
//  2. auto のときは環境変数 NO_COLOR (無効) > FORCE_COLOR (有効) の順で見る。
//  3. いずれも無ければ stdout が TTY のときだけ色を出す。
//
// NO_COLOR / FORCE_COLOR は「設定されていて値が空でない」ときに効く
// (NO_COLOR の慣習は https://no-color.org/)。watch などパイプ経由で色を出したいときは
// `--color=always` か FORCE_COLOR を使う。
func colorEnabled(mode string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	}
	if v, ok := os.LookupEnv("NO_COLOR"); ok && v != "" {
		return false
	}
	if v, ok := os.LookupEnv("FORCE_COLOR"); ok && v != "" {
		return true
	}
	return isTTY(os.Stdout)
}

func newColors() colors {
	if !colorEnabled(colorMode) {
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
		warn:   "\033[33m",
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

// formatSyncStatus はストアの同期状況を 1 行サマリに整形する。
// クリーンなら緑、未同期があれば黄、git 管理外/upstream 未設定は淡色で注記する。
func formatSyncStatus(s SyncStatus, c colors) string {
	if s.NotGit {
		return c.dim + "git 管理されていません (git init とリモート設定が必要)" + c.reset
	}
	if s.Clean() {
		return c.done + "クリーン (同期済み)" + c.reset + c.dim + " — " + s.Upstream + c.reset
	}

	var parts []string
	if s.Dirty > 0 {
		parts = append(parts, fmt.Sprintf("未コミット %d ファイル", s.Dirty))
	}
	if s.NoUpstream {
		parts = append(parts, "upstream 未設定 (未 push)")
	} else {
		if s.Ahead > 0 {
			parts = append(parts, fmt.Sprintf("未 push %d コミット", s.Ahead))
		}
		if s.Behind > 0 {
			parts = append(parts, fmt.Sprintf("未取得 %d コミット", s.Behind))
		}
	}
	if len(parts) == 0 {
		// Dirty=0 かつ ahead/behind=0 だが upstream あり → Clean が拾うのでここには来ない。
		parts = append(parts, "未同期")
	}

	summary := c.warn + strings.Join(parts, " / ") + c.reset
	ref := ""
	if s.Upstream != "" {
		ref = c.dim + " (" + s.Upstream + ")" + c.reset
	}
	return summary + ref
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

func (t *table) render(w io.Writer, c colors) {
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

// truncateDisp は表示幅が max を超える場合に末尾を "…" で丸める (CJK 幅対応)。
// max 以内ならそのまま返す。max が小さすぎる (1 以下) ときは "…" を返す。
func truncateDisp(s string, max int) string {
	if dispWidth(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	w := 0
	for i, r := range s {
		rw := 1
		if isWide(r) {
			rw = 2
		}
		if w+rw > max-1 { // 末尾の "…" (幅1) の分を空ける
			return s[:i] + "…"
		}
		w += rw
	}
	return s
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
