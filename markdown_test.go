package main

import (
	"strings"
	"testing"
)

// mdLines はレンダリング結果を装飾抜きの行に落とす (装飾の有無ではなく行組みを検証する)。
func mdLines(s string) []string {
	return strings.Split(stripANSI(s), "\n")
}

// TestRenderMarkdownStripsMarkers は見出し・強調・インラインコードの記号が消え、
// 中身のテキストだけが残ることを確認する。
func TestRenderMarkdownStripsMarkers(t *testing.T) {
	src := "## やりたいこと\n\n**太字** と `code` と *斜体* を含む段落。"
	out := stripANSI(renderMarkdown(src, 60))
	for _, marker := range []string{"##", "**", "`", "*"} {
		if strings.Contains(out, marker) {
			t.Errorf("記号 %q が残っている:\n%s", marker, out)
		}
	}
	for _, want := range []string{"やりたいこと", "太字", "code", "斜体"} {
		if !strings.Contains(out, want) {
			t.Errorf("本文 %q が失われている:\n%s", want, out)
		}
	}
}

// TestRenderMarkdownFitsWidth はどの行も指定幅を超えないことを確認する
// (viewport は長い行を折り返さず横に切り捨てるため、幅超過は「読めない行」になる)。
func TestRenderMarkdownFitsWidth(t *testing.T) {
	src := strings.Join([]string{
		"# 要件",
		"",
		"日本語の長い段落を書きます。ここには空白が無いので語境界での折返しができません。",
		"英語 with a very long sentence that must also wrap correctly at the given width.",
		"",
		"- 箇条書きの項目も同じように折り返される必要があります (継続行はぶら下げる)",
		"  - ネストした項目もインデントぶんだけ幅が減ることを考慮する",
		"",
		"```sh",
		"very long command --with --many --flags --that --exceed --the --pane --width",
		"```",
		"",
		"> 引用も折り返す。引用の縦線ぶんだけ本文の幅が狭くなる。",
	}, "\n")
	for _, w := range []int{20, 34, 60} {
		for _, line := range mdLines(renderMarkdown(src, w)) {
			if dispWidth(line) > w {
				t.Errorf("width=%d を超える行がある (dispWidth=%d): %q", w, dispWidth(line), line)
			}
		}
	}
}

// TestRenderMarkdownListHangingIndent は箇条書きの継続行が本文の桁にそろう
// (マーカーの下に潜り込まない) ことを確認する。
func TestRenderMarkdownListHangingIndent(t *testing.T) {
	src := "- これは折り返しが必要なくらい長い日本語の箇条書き項目です。継続行はぶら下げる。"
	lines := mdLines(renderMarkdown(src, 24))
	if len(lines) < 2 {
		t.Fatalf("折り返されていない: %q", lines)
	}
	indent := dispWidth("•") + 1
	for _, line := range lines[1:] {
		if got := len(line) - len(strings.TrimLeft(line, " ")); got != indent {
			t.Errorf("継続行のぶら下げ幅が違う: want=%d got=%d line=%q", indent, got, line)
		}
	}
}

// TestRenderMarkdownJoinsHardWrappedLines はソース側で手折返しされた段落を 1 本に
// 連結してから幅で折り直すことを確認する (行ごとに折ると長短が交互に並ぶため)。
// 和文どうしは詰めて繋ぎ、欧文どうしは空白を挟む。
func TestRenderMarkdownJoinsHardWrappedLines(t *testing.T) {
	out := stripANSI(renderMarkdown("あいうえお\nかきくけこ", 40))
	if out != "あいうえおかきくけこ" {
		t.Errorf("和文の連結で余計な空白が入った: %q", out)
	}
	out = stripANSI(renderMarkdown("hello\nworld", 40))
	if out != "hello world" {
		t.Errorf("欧文の連結が空白で繋がっていない: %q", out)
	}
	// 見出し・箇条書きは別ブロックなので連結しない。
	lines := mdLines(renderMarkdown("段落です\n- 項目", 40))
	if len(lines) != 2 {
		t.Errorf("段落と箇条書きが連結されている: %q", lines)
	}
}

// TestRenderMarkdownCodeBlockIsLiteral はコードブロックの中身を Markdown として
// 解釈しない (記号がそのまま見える) ことを確認する。
func TestRenderMarkdownCodeBlockIsLiteral(t *testing.T) {
	out := stripANSI(renderMarkdown("```\n# comment **not bold**\n```", 60))
	if !strings.Contains(out, "# comment **not bold**") {
		t.Errorf("コードブロックの中身が加工されている: %q", out)
	}
	if strings.Contains(out, "```") {
		t.Errorf("フェンス行が残っている: %q", out)
	}
}

// TestRenderMarkdownNoLineStartsWithKinsoku は行頭禁則 (、。 で行が始まらない) を確認する。
func TestRenderMarkdownNoLineStartsWithKinsoku(t *testing.T) {
	src := "折返し位置に句読点が来る文章をいくつも並べて、境界をずらしながら検証する。ここでも、区切りが来る。"
	for w := 12; w <= 40; w++ {
		for _, line := range mdLines(renderMarkdown(src, w)) {
			if line == "" {
				continue
			}
			if head := []rune(line)[0]; strings.ContainsRune("、。", head) {
				t.Errorf("width=%d で行頭に約物が来た: %q", w, line)
			}
		}
	}
}

// TestRenderMarkdownLinkKeepsURL はリンクを「テキスト + URL」に展開する
// (TUI ではリンクを踏めないので URL 自体を見せる) ことを確認する。
func TestRenderMarkdownLinkKeepsURL(t *testing.T) {
	out := stripANSI(renderMarkdown("[PR](https://example.com/pull/1) を参照", 60))
	if !strings.Contains(out, "PR") || !strings.Contains(out, "https://example.com/pull/1") {
		t.Errorf("リンクのテキストか URL が失われた: %q", out)
	}
	if strings.ContainsAny(out, "[]") {
		t.Errorf("リンクの記号が残っている: %q", out)
	}
}

func TestSplitFrontmatter(t *testing.T) {
	meta, body := splitFrontmatter("---\nid: \"0001\"\ntitle: テスト\n---\n\n# 要件\n本文\n")
	if meta != "id: \"0001\"\ntitle: テスト" {
		t.Errorf("frontmatter の切り出しが違う: %q", meta)
	}
	if body != "# 要件\n本文\n" {
		t.Errorf("本文の切り出しが違う: %q", body)
	}
	// frontmatter が無いファイルは全体が本文。
	if meta, body := splitFrontmatter("# 要件\n"); meta != "" || body != "# 要件\n" {
		t.Errorf("frontmatter 無しの扱いが違う: meta=%q body=%q", meta, body)
	}
	// 終端が無いものは frontmatter とみなさない (本文を落とさない)。
	if meta, body := splitFrontmatter("---\nid: \"0001\"\n"); meta != "" || body == "" {
		t.Errorf("未終端 frontmatter で本文が失われた: meta=%q body=%q", meta, body)
	}
}

// TestTuiDetailRendersFrontmatterAsMeta は frontmatter が水平線に化けず、
// key: value のまま読めることを確認する。
func TestTuiDetailRendersFrontmatterAsMeta(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/0001-x.md"
	writeFile(t, path, "---\nid: \"0001\"\nstatus: todo\n---\n\n# 要件\n本文です。\n")
	out := stripANSI(tuiDetail(Task{Path: path, ID: "0001", Status: "todo"}, 40))
	if !strings.Contains(out, "status: todo") {
		t.Errorf("frontmatter が key: value で出ていない:\n%s", out)
	}
	if !strings.Contains(out, "要件") || !strings.Contains(out, "本文です。") {
		t.Errorf("本文が出ていない:\n%s", out)
	}
	for _, line := range mdLines(out) {
		if dispWidth(line) > 40 {
			t.Errorf("幅を超える行がある: %q", line)
		}
	}
}
