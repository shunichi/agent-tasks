package main

import (
	"strings"
	"testing"
)

// withTerminalWidth は terminalWidth を一時的に固定値へ差し替える (テスト後に復元)。
func withTerminalWidth(t *testing.T, w int) {
	t.Helper()
	orig := terminalWidth
	terminalWidth = func() int { return w }
	t.Cleanup(func() { terminalWidth = orig })
}

// maxLineWidth はレンダ結果の各行の最大表示幅を返す (色なし colors 前提で ANSI を含まない)。
func maxLineWidth(s string) int {
	max := 0
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if w := dispWidth(line); w > max {
			max = w
		}
	}
	return max
}

func renderTable(t *table) string {
	var b strings.Builder
	t.render(&b, colors{})
	return b.String()
}

func TestTableTruncatesTitleToTerminalWidth(t *testing.T) {
	withTerminalWidth(t, 40)

	tbl := newTable("ID", "TITLE", "UPDATED").truncatable(1)
	long := strings.Repeat("あ", 50) // 表示幅 100 の長い TITLE
	tbl.add(cell{"0001", ""}, cell{long, ""}, cell{"2026-06-30", ""})

	out := renderTable(tbl)
	if w := maxLineWidth(out); w > 40 {
		t.Errorf("行幅 %d が端末幅 40 を超えた:\n%s", w, out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("TITLE が truncate されていない (… が無い):\n%s", out)
	}
	// UPDATED 列が残っている (TITLE に押し出されず収まる)。
	if !strings.Contains(out, "2026-06-30") {
		t.Errorf("UPDATED 列が欠落:\n%s", out)
	}
}

func TestTableNoTruncateWhenWidthUnknown(t *testing.T) {
	withTerminalWidth(t, 0) // 端末幅が取れない (パイプ等)

	tbl := newTable("ID", "TITLE", "UPDATED").truncatable(1)
	long := strings.Repeat("x", 80)
	tbl.add(cell{"0001", ""}, cell{long, ""}, cell{"2026-06-30", ""})

	out := renderTable(tbl)
	if !strings.Contains(out, long) {
		t.Errorf("端末幅不明のとき TITLE を素のまま出すべきだが truncate された:\n%s", out)
	}
	if strings.Contains(out, "…") {
		t.Errorf("端末幅不明のとき truncate してはいけない:\n%s", out)
	}
}

func TestTableNoTruncateWhenTitleFits(t *testing.T) {
	withTerminalWidth(t, 200) // 十分広い

	tbl := newTable("ID", "TITLE", "UPDATED").truncatable(1)
	title := "短い title"
	tbl.add(cell{"0001", ""}, cell{title, ""}, cell{"2026-06-30", ""})

	out := renderTable(tbl)
	if !strings.Contains(out, title) {
		t.Errorf("収まる TITLE は truncate しないべき:\n%s", out)
	}
	if strings.Contains(out, "…") {
		t.Errorf("収まる TITLE に … が付いた:\n%s", out)
	}
}

// 残り幅が 1 未満 (他列だけで端末幅を食い切る) のときは truncate しない (素のまま)。
func TestTableNoTruncateWhenNoRoom(t *testing.T) {
	withTerminalWidth(t, 5) // ID 列 + ギャップだけで超える狭さ

	tbl := newTable("ID", "TITLE", "UPDATED").truncatable(1)
	tbl.add(cell{"0001", ""}, cell{"hello world", ""}, cell{"2026-06-30", ""})

	out := renderTable(tbl)
	if !strings.Contains(out, "hello world") {
		t.Errorf("残り幅が無いときは truncate せず素のまま出すべき:\n%s", out)
	}
}

func TestTableTruncDisabledByDefault(t *testing.T) {
	withTerminalWidth(t, 20)

	tbl := newTable("ID", "TITLE") // truncatable を呼ばない
	long := strings.Repeat("y", 60)
	tbl.add(cell{"0001", ""}, cell{long, ""})

	out := renderTable(tbl)
	if !strings.Contains(out, long) {
		t.Errorf("truncatable 未設定のテーブルは truncate しないべき:\n%s", out)
	}
}
