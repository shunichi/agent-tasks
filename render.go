package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
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
	return isStdoutTTY()
}

// isStdoutTTY は auto モードの最終判定 (stdout が端末か) を返す。変数にしておくことで
// テストから差し替えられ、実 stdout の TTY 状態 (ターミナル実行か CI/パイプ経由か) に
// テスト結果が左右されないようにする。
var isStdoutTTY = func() bool { return isTTY(os.Stdout) }

// terminalWidth は stdout の端末桁数を返す。取得できない (パイプ等で端末でない) ときは 0。
// 優先順位: ioctl(TIOCGWINSZ) で実寸を取り、ダメなら COLUMNS 環境変数、いずれも無ければ 0。
// 0 のときテーブルは TITLE を truncate しない (素のまま出す)。変数にしてテストから差し替え可能。
var terminalWidth = func() int {
	if w := ttyWidth(os.Stdout.Fd()); w > 0 {
		return w
	}
	if v := strings.TrimSpace(os.Getenv("COLUMNS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func newColors() colors {
	if !colorEnabled(colorMode) {
		return colors{}
	}
	return palette()
}

// palette は色付きの colors を返す (有効/無効の判定抜き)。
// statusline のように「TTY でなくても色を出したい」用途が colorEnabled を介さず使う。
func palette() colors {
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
	headers  []string
	rows     [][]cell
	truncCol int // 端末幅に収めるため truncate する列 index。-1 で無効。
}

func newTable(headers ...string) *table {
	return &table{headers: headers, truncCol: -1}
}

// truncatable は端末幅に収まらないとき col 番目の列を残り幅で truncate するよう設定する
// (他列の幅は維持)。端末幅が取れないときは何もしない。長い TITLE が watch (折返し OFF) で
// 桁ずれ・残骸を出すのを防ぐ用途。
func (t *table) truncatable(col int) *table {
	t.truncCol = col
	return t
}

func (t *table) add(cells ...cell) {
	t.rows = append(t.rows, cells)
}

const tableGap = 2 // 列間の空白幅

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

	const gap = tableGap
	t.truncateColumn(widths, gap)

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

// truncateColumn は truncatable に指定された列を、端末幅に収まるよう残り幅で truncate する。
// 端末幅が取れない (0) / 指定なし / 既に収まっているときは何もしない。widths を破壊的に縮め、
// 対象列のヘッダと各行セルも truncate して、render 時のパディング計算と整合させる。
func (t *table) truncateColumn(widths []int, gap int) {
	col := t.truncCol
	if col < 0 || col >= len(widths) {
		return
	}
	tw := terminalWidth()
	if tw <= 0 {
		return
	}
	// 対象列以外の幅合計 + 列間ギャップ (n-1 個)。残りを対象列に割り当てる。
	other := gap * (len(widths) - 1)
	for i, wd := range widths {
		if i != col {
			other += wd
		}
	}
	avail := tw - other
	if avail < 1 || widths[col] <= avail {
		return // 残り幅が無い / 既に収まっている
	}
	widths[col] = avail
	t.headers[col] = truncateDisp(t.headers[col], avail)
	for _, row := range t.rows {
		if col < len(row) {
			row[col].text = truncateDisp(row[col].text, avail)
		}
	}
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

// truncateDisp は表示幅が max を超える場合に末尾を "…" で丸める (CJK / 絵文字幅対応)。
// max 以内ならそのまま返す。max が小さすぎる (1 以下) ときは "…" を返す。
// 文字幅は go-runewidth に委譲する (絵文字 2 / 結合・ゼロ幅 0 などを正しく数える)。
func truncateDisp(s string, max int) string {
	if dispWidth(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	w := 0
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > max-1 { // 末尾の "…" (幅1) の分を空ける
			return s[:i] + "…"
		}
		w += rw
	}
	return s
}

// dispWidth は端末上の表示幅を返す (go-runewidth に委譲)。CJK / 全角・絵文字は 2、
// 結合文字・ゼロ幅文字は 0 と数える。表計算 (列幅・パディング) の単一の幅基準。
func dispWidth(s string) int {
	return runewidth.StringWidth(s)
}
