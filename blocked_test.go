package main

import (
	"os"
	"testing"
	"time"
)

func TestHumanizeSince(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		since string
		want  string
	}{
		{"数日前", "2026-06-26T12:00:00Z", "3d"},
		{"数時間前", "2026-06-29T07:00:00Z", "5h"},
		{"数分前", "2026-06-29T11:48:00Z", "12m"},
		{"直近", "2026-06-29T11:59:30Z", "now"},
		{"日付のみ (旧形式)", "2026-06-22", "7d"},
		{"未来は0扱い", "2026-06-30T12:00:00Z", "now"},
		{"空は空", "", ""},
		{"解析不能は空", "not-a-date", ""},
	}
	for _, tc := range cases {
		if got := humanizeSince(tc.since, now); got != tc.want {
			t.Errorf("%s: humanizeSince(%q) = %q, want %q", tc.name, tc.since, got, tc.want)
		}
	}
}

func TestBlockedCell(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	c := colors{dim: "<dim>", block: "<block>", reset: "<r>"}

	// blocked 以外は空セル。
	if got := blockedCell(Task{Status: "todo"}, c, now); got.text != "" {
		t.Errorf("todo は空セルのはず、got %+v", got)
	}

	// blocked_at 未記録は "?"。
	if got := blockedCell(Task{Status: "blocked"}, c, now); got.text != "?" {
		t.Errorf("blocked_at 無しは ?、got %+v", got)
	}

	// 閾値内は dim。
	recent := Task{Status: "blocked", BlockedAt: "2026-06-28T12:00:00Z"}
	if got := blockedCell(recent, c, now); got.text != "1d" || got.color != c.dim {
		t.Errorf("最近の blocked = {1d, dim}、got %+v", got)
	}

	// 閾値 (7d) 超えは警告色 (block)。
	stale := Task{Status: "blocked", BlockedAt: "2026-06-10T12:00:00Z"}
	if got := blockedCell(stale, c, now); got.text != "19d" || got.color != c.block {
		t.Errorf("長期 blocked = {19d, block}、got %+v", got)
	}
}

func TestBlockedTitle(t *testing.T) {
	// blocked かつ理由ありは title に括弧書きで添える。
	bt := blockedTitle(Task{Status: "blocked", Title: "やること", BlockedReason: "確認待ち"})
	if bt != "やること  (確認待ち)" {
		t.Errorf("blockedTitle = %q", bt)
	}
	// blocked でも理由が無ければ title のまま。
	if got := blockedTitle(Task{Status: "blocked", Title: "やること"}); got != "やること" {
		t.Errorf("理由無し = %q", got)
	}
	// blocked 以外は理由があっても添えない。
	if got := blockedTitle(Task{Status: "todo", Title: "やること", BlockedReason: "x"}); got != "やること" {
		t.Errorf("非 blocked = %q", got)
	}
}

func TestTruncateDisp(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},     // max 以内はそのまま
		{"abcdefghij", 5, "abcd…"}, // ASCII は max-1 まで残して …
		{"あいうえお", 10, "あいうえお"},     // 全角5字=幅10、ちょうど収まる
		{"あいうえお", 6, "あい…"},        // 幅6: あい(4)+…(1)=5、う を入れると 6+1 で超える
		{"x", 1, "x"},              // 幅1で max1 はそのまま
		{"xy", 1, "…"},             // max<=1 で超過は …
	}
	for _, tc := range cases {
		if got := truncateDisp(tc.in, tc.max); got != tc.want {
			t.Errorf("truncateDisp(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

func TestParseTaskBlockedFields(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/0001-x.md"
	content := "---\nid: \"0001\"\nstatus: blocked\nblocked_at: \"2026-06-29T02:20:05+09:00\"\nblocked_reason: 0020 のマージ待ち\n---\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.BlockedAt != "2026-06-29T02:20:05+09:00" {
		t.Errorf("BlockedAt = %q", got.BlockedAt)
	}
	if got.BlockedReason != "0020 のマージ待ち" {
		t.Errorf("BlockedReason = %q", got.BlockedReason)
	}
}
