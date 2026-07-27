package main

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// TUI 詳細ペイン用の軽量 Markdown レンダラ。
//
// glamour (goldmark + chroma) を採らなかった理由: 依存が重い割に、ここで扱うのは
// タスクファイルという既知の狭いサブセット (見出し / 箇条書き / コードブロック / 引用 /
// 強調 / インラインコード / リンク) でしかなく、かつ本文が日本語主体なので折返しを
// 自前で握りたい。CJK は語境界が無いので任意位置で折れる一方、行頭に来てはいけない
// 約物 (、。) があり、汎用ワードラップだと不自然な行が出る。幅は既存の描画と同じ
// go-runewidth (dispWidth) に委譲し、一覧テーブルと同じ幅解釈で揃える。
//
// 不変条件: 返す各行の dispWidth は width 以下 (viewport は長い行を折り返さず
// 横に切り捨てるため、ここで幅を保証しないと読めない行が出る)。

type mdStyleID int

const (
	mdPlain mdStyleID = iota
	mdBold
	mdItalic
	mdCode
	mdLinkText
	mdLinkURL
	mdH1
	mdH2
	mdH3
	mdMarker // 箇条書きマーカー・罫線・引用の縦線
	mdQuote
	mdMetaKey
)

var mdStyles = [...]lipgloss.Style{
	mdPlain:    lipgloss.NewStyle(),
	mdBold:     lipgloss.NewStyle().Bold(true),
	mdItalic:   lipgloss.NewStyle().Italic(true),
	mdCode:     lipgloss.NewStyle().Foreground(lipgloss.Color("3")), // yellow
	mdLinkText: lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Underline(true),
	mdLinkURL:  lipgloss.NewStyle().Faint(true),
	mdH1:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")), // cyan
	mdH2:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3")), // yellow
	mdH3:       lipgloss.NewStyle().Bold(true),
	mdMarker:   lipgloss.NewStyle().Faint(true),
	mdQuote:    lipgloss.NewStyle().Faint(true),
	mdMetaKey:  lipgloss.NewStyle().Faint(true),
}

func (id mdStyleID) render(s string) string {
	if s == "" {
		return ""
	}
	return mdStyles[id].Render(s)
}

// mdRun は同一装飾が続くテキストの断片。
type mdRun struct {
	text string
	sid  mdStyleID
}

// mdPrefix は行頭に付く装飾済みの飾り (箇条書きマーカー / 引用の縦線 / ぶら下げ用の空白)。
// styled と width を分けて持つのは、ANSI エスケープを含む文字列から幅を測り直さずに
// 済ませるため。
type mdPrefix struct {
	styled string
	width  int
}

func mdSpaces(n int) mdPrefix { return mdPrefix{styled: strings.Repeat(" ", n), width: n} }

var (
	mdHeadingRe = regexp.MustCompile(`^\s{0,3}(#{1,6})\s+(.*?)\s*#*$`)
	mdFenceRe   = regexp.MustCompile("^\\s{0,3}(```|~~~)")
	mdListRe    = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+(.*)$`)
	mdLinkRe    = regexp.MustCompile(`^\[([^\]\n]*)\]\(([^)\s]+)\)`)
)

// renderMarkdown は Markdown 本文を width 幅の装飾済みテキストへ整形する。
func renderMarkdown(src string, width int) string {
	if width < 8 {
		width = 8
	}
	lines := strings.Split(strings.ReplaceAll(src, "\t", "    "), "\n")
	var out []string
	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			out = append(out, "")
			i++
		case mdFenceRe.MatchString(line):
			i = mdRenderFence(lines, i, width, &out)
		case mdIsHR(line):
			out = append(out, mdMarker.render(mdRepeatTo("─", width)))
			i++
		case mdHeadingRe.MatchString(line):
			m := mdHeadingRe.FindStringSubmatch(line)
			sid := mdH3
			switch len(m[1]) {
			case 1:
				sid = mdH1
			case 2:
				sid = mdH2
			}
			out = append(out, mdWrap(mdInline(m[2], sid), width, mdSpaces(0), mdSpaces(0))...)
			i++
		case strings.HasPrefix(trimmed, ">"):
			i = mdRenderQuote(lines, i, width, &out)
		case mdListRe.MatchString(line):
			i = mdRenderListItem(lines, i, width, &out)
		case strings.HasPrefix(trimmed, "|"):
			// 表は桁を崩さないほうが読めるので整形せず、幅だけ守って出す。
			out = append(out, mdChunk(line, width, mdPlain)...)
			i++
		default:
			i = mdRenderParagraph(lines, i, width, &out)
		}
	}
	return strings.Join(out, "\n")
}

// mdRenderFence はフェンス付きコードブロックを出力する。中身は Markdown として
// 解釈せず (記号をそのまま見せたい場所なので)、幅だけ守って縦線を添える。
func mdRenderFence(lines []string, i, width int, out *[]string) int {
	fence := strings.TrimSpace(lines[i])[:3]
	bar := mdPrefix{styled: mdMarker.render("│ "), width: dispWidth("│ ")}
	i++
	for ; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), fence) {
			i++ // 閉じフェンスは表示しない
			break
		}
		for _, l := range mdChunk(lines[i], width-bar.width, mdCode) {
			*out = append(*out, bar.styled+l)
		}
	}
	return i
}

// mdRenderQuote は引用ブロック (連続する "> " 行) を 1 段落として折り返す。
func mdRenderQuote(lines []string, i, width int, out *[]string) int {
	var text string
	for ; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, ">") {
			break
		}
		text = mdJoin(text, strings.TrimSpace(strings.TrimPrefix(t, ">")))
	}
	bar := mdPrefix{styled: mdMarker.render("│ "), width: dispWidth("│ ")}
	*out = append(*out, mdWrap(mdInline(text, mdQuote), width, bar, bar)...)
	return i
}

var mdBullets = []string{"•", "◦", "‣", "·"}

// mdRenderListItem は箇条書き 1 項目 (継続行を含む) を、マーカー幅ぶんぶら下げて折り返す。
func mdRenderListItem(lines []string, i, width int, out *[]string) int {
	m := mdListRe.FindStringSubmatch(lines[i])
	indent, marker, text := len(m[1]), m[2], m[3]
	level := min(indent/2, len(mdBullets)-1)
	if !strings.ContainsAny(marker, "-*+") {
		marker = strings.TrimSuffix(strings.TrimSuffix(marker, "."), ")") + "."
	} else {
		marker = mdBullets[level]
	}
	i++
	// 継続行 (次の項目でも別ブロックでもない、字下げされた行) を本文へ畳み込む。
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" || mdStartsBlock(lines[i]) {
			break
		}
		text = mdJoin(text, strings.TrimSpace(lines[i]))
	}
	pad := strings.Repeat(" ", level*2)
	head := mdPrefix{styled: pad + mdMarker.render(marker) + " ", width: level*2 + dispWidth(marker) + 1}
	*out = append(*out, mdWrap(mdInline(text, mdPlain), width, head, mdSpaces(head.width))...)
	return i
}

// mdRenderParagraph は段落 (空行または別ブロックまでの連続行) を 1 本の文字列に
// 連結してから折り返す。タスクファイルの本文はソース側で 100 桁前後に手で折り返して
// あるので、行ごとに折り返すとペイン幅では長短が交互に並んで読みにくい。連結して
// から幅に合わせ直す。
func mdRenderParagraph(lines []string, i, width int, out *[]string) int {
	var text string
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" || (text != "" && mdStartsBlock(lines[i])) {
			break
		}
		text = mdJoin(text, strings.TrimSpace(lines[i]))
	}
	*out = append(*out, mdWrap(mdInline(text, mdPlain), width, mdSpaces(0), mdSpaces(0))...)
	return i
}

// mdIsHR は水平線 (---, ***, ___) を判定する。RE2 は後方参照を持たないので手で見る。
func mdIsHR(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	c := t[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	n := 0
	for i := range len(t) {
		switch t[i] {
		case c:
			n++
		case ' ', '\t':
		default:
			return false
		}
	}
	return n >= 3
}

// mdStartsBlock は段落・箇条書きの継続行として畳み込めない行 (別ブロックの開始) を判定する。
func mdStartsBlock(line string) bool {
	trimmed := strings.TrimSpace(line)
	return mdListRe.MatchString(line) || mdHeadingRe.MatchString(line) ||
		mdFenceRe.MatchString(line) || mdIsHR(line) ||
		strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "|")
}

// mdJoin は行を段落へ連結する。和文どうしは詰めて繋ぎ (スペースを入れると不自然)、
// 欧文どうしは空白を挟む。
func mdJoin(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	prev := []rune(a)[len([]rune(a))-1]
	next := []rune(b)[0]
	if dispWidth(string(prev)) == 2 || dispWidth(string(next)) == 2 {
		return a + b
	}
	return a + " " + b
}

// mdInline は 1 段落ぶんのテキストを装飾つき run 列へ分解する。base は装飾なし部分に
// 使う装飾 (見出しや引用では段落全体に色が乗る)。
func mdInline(s string, base mdStyleID) []mdRun {
	var runs []mdRun
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			runs = append(runs, mdRun{text: buf.String(), sid: base})
			buf.Reset()
		}
	}
	emit := func(text string, sid mdStyleID) {
		if text != "" {
			runs = append(runs, mdRun{text: text, sid: sid})
		}
	}
	for i := 0; i < len(s); {
		rest := s[i:]
		switch {
		case rest[0] == '`':
			if j := strings.IndexByte(rest[1:], '`'); j > 0 {
				flush()
				emit(rest[1:1+j], mdCode)
				i += j + 2
				continue
			}
		case strings.HasPrefix(rest, "**"):
			if j := strings.Index(rest[2:], "**"); j > 0 {
				flush()
				emit(rest[2:2+j], mdBold)
				i += j + 4
				continue
			}
		case rest[0] == '*':
			// 単独の "*" は強調。閉じが無ければただの記号として落とさず出す。
			if j := strings.IndexByte(rest[1:], '*'); j > 0 {
				flush()
				emit(rest[1:1+j], mdItalic)
				i += j + 2
				continue
			}
		case rest[0] == '[':
			if m := mdLinkRe.FindStringSubmatch(rest); m != nil {
				flush()
				emit(m[1], mdLinkText)
				emit(" ("+m[2]+")", mdLinkURL)
				i += len(m[0])
				continue
			}
		}
		buf.WriteByte(s[i])
		i++
	}
	flush()
	return runs
}

// mdChar は折返し計算のために 1 文字ぶんの装飾と表示幅を持つ。
type mdChar struct {
	r   rune
	sid mdStyleID
	w   int
}

// mdWrap は run 列を width に収まる行へ折り返す。first は 1 行目、cont は 2 行目以降の
// 行頭に付く飾りで、箇条書きのぶら下げインデントはこの差で表現する。
func mdWrap(runs []mdRun, width int, first, cont mdPrefix) []string {
	var chars []mdChar
	for _, run := range runs {
		for _, r := range run.text {
			chars = append(chars, mdChar{r: r, sid: run.sid, w: dispWidth(string(r))})
		}
	}
	if len(chars) == 0 {
		return []string{""}
	}
	// 全角 1 文字が必ず入る余白を残す (残さないと 1 文字も入らず、幅を超える行が出る)。
	if maxPrefix := max(0, width-2); first.width > maxPrefix || cont.width > maxPrefix {
		first, cont = mdSpaces(min(first.width, maxPrefix)), mdSpaces(min(cont.width, maxPrefix))
	}
	var out []string
	for i := 0; i < len(chars); {
		prefix := cont
		if len(out) == 0 {
			prefix = first
		}
		avail := max(1, width-prefix.width)
		for i < len(chars) && chars[i].r == ' ' && len(out) > 0 {
			i++ // 折返し位置に来た空白は行頭に残さない
		}
		if i >= len(chars) {
			break
		}
		w, end, lastBreak := 0, i, -1
		for j := i; j < len(chars); j++ {
			if w+chars[j].w > avail {
				break
			}
			w += chars[j].w
			end = j + 1
			if j+1 < len(chars) && mdCanBreak(chars[j].r, chars[j+1].r) {
				lastBreak = j + 1
			}
		}
		if end == i {
			end = i + 1 // 1 文字も入らない極端な幅でも必ず前進する
		}
		if end < len(chars) && !mdCanBreak(chars[end-1].r, chars[end].r) && lastBreak > i {
			end = lastBreak // 語の途中で切らずに直前の分割可能位置まで戻す
		}
		out = append(out, prefix.styled+mdJoinChars(chars[i:end]))
		i = end
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// mdJoinChars は同じ装飾が続く範囲をまとめて 1 回だけ装飾を適用する
// (1 文字ごとに装飾すると ANSI が肥大して viewport のスクロールが重くなる)。
func mdJoinChars(chars []mdChar) string {
	var b strings.Builder
	var buf strings.Builder
	sid := chars[0].sid
	for _, c := range chars {
		if c.sid != sid {
			b.WriteString(sid.render(buf.String()))
			buf.Reset()
			sid = c.sid
		}
		buf.WriteRune(c.r)
	}
	b.WriteString(sid.render(strings.TrimRight(buf.String(), " ")))
	return b.String()
}

// 行頭に置かない約物 (禁則の最小セット) と、直後で折り返さない開き約物。
const (
	mdNoBreakBefore = "、。，．,.:;!?！？）)」』】〉》］]｝}’”ー々〜:;"
	mdNoBreakAfter  = "（(「『【〈《［[｛{‘“"
)

// mdCanBreak は prev と next の間で改行してよいかを返す。欧文は空白でのみ折り、
// 和文は任意位置で折る (語境界が無いため) が、行頭禁則の約物だけは避ける。
func mdCanBreak(prev, next rune) bool {
	if prev == ' ' || next == ' ' {
		return true
	}
	if strings.ContainsRune(mdNoBreakBefore, next) || strings.ContainsRune(mdNoBreakAfter, prev) {
		return false
	}
	return dispWidth(string(prev)) == 2 || dispWidth(string(next)) == 2
}

// mdChunk は 1 行を折り返さず幅で機械的に分割する (コードブロック・表など、語の
// 区切りより桁を優先したい場所で使う)。
func mdChunk(line string, width int, sid mdStyleID) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	var buf strings.Builder
	w := 0
	for _, r := range line {
		rw := dispWidth(string(r))
		if w+rw > width {
			out = append(out, sid.render(buf.String()))
			buf.Reset()
			w = 0
		}
		buf.WriteRune(r)
		w += rw
	}
	out = append(out, sid.render(buf.String()))
	return out
}

// mdRepeatTo は文字 s を表示幅 width に収まるだけ繰り返す (罫線用。"─" は
// East Asian ambiguous なので幅 1 とは限らない)。
func mdRepeatTo(s string, width int) string {
	w := dispWidth(s)
	if w < 1 {
		return ""
	}
	return strings.Repeat(s, width/w)
}

// splitFrontmatter は YAML frontmatter と本文を分ける。frontmatter が無ければ meta は空。
func splitFrontmatter(src string) (meta, body string) {
	s := strings.TrimPrefix(src, "\ufeff")
	if !strings.HasPrefix(s, "---\n") {
		return "", s
	}
	lines := strings.Split(s[len("---\n"):], "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			return strings.Join(lines[:i], "\n"), strings.TrimLeft(strings.Join(lines[i+1:], "\n"), "\n")
		}
	}
	return "", s // 終端が無ければ frontmatter とみなさない (全部を本文として出す)
}

// renderFrontmatter は frontmatter を Markdown として解釈せず (先頭の "---" が
// 水平線に化けるのを避ける)、key を淡色にした素の key: value として出す。
func renderFrontmatter(meta string, width int) string {
	var out []string
	for line := range strings.SplitSeq(meta, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(strings.TrimSpace(line), "-") || strings.HasPrefix(strings.TrimSpace(line), "#") {
			out = append(out, mdWrap([]mdRun{{text: strings.TrimSpace(line), sid: mdPlain}}, width, mdSpaces(2), mdSpaces(4))...)
			continue
		}
		runs := []mdRun{{text: key + ":", sid: mdMetaKey}, {text: val, sid: mdPlain}}
		out = append(out, mdWrap(runs, width, mdSpaces(0), mdSpaces(2))...)
	}
	return strings.Join(out, "\n")
}
