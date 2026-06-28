package main

import (
	"strings"
	"testing"
)

func TestOverwriteFrame(t *testing.T) {
	got := overwriteFrame("line1\nline2\n")

	// ちらつきの原因になる全画面消去は使わない (これがこのタスクの肝)。
	if strings.Contains(got, "\033[2J") {
		t.Error("overwriteFrame が全消去 \\033[2J を含んでいる (ちらつきの原因)")
	}
	// カーソルを左上へ戻して上書きする。
	if !strings.Contains(got, "\033[H") {
		t.Error("カーソルホーム \\033[H が無い")
	}
	// 各行末を \033[K でクリア (2 行 → 2 個)。
	if n := strings.Count(got, "\033[K"); n != 2 {
		t.Errorf("行末クリア \\033[K の数 = %d, want 2", n)
	}
	// 末尾でカーソル以下をクリア (行数が減ったときの消し残し対策)。
	if !strings.HasSuffix(got, "\033[J") {
		t.Errorf("末尾が \\033[J でない: %q", got)
	}
	// 中身は保持される。
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") {
		t.Error("行の内容が欠落している")
	}
}
