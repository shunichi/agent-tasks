package main

import "testing"

func TestDisplayDate(t *testing.T) {
	cases := map[string]string{
		"2026-06-29T02:58:22+09:00": "2026-06-29", // 日時 → 日付に丸める
		"2026-06-28T17:58:22Z":      "2026-06-28", // UTC でも日付部分
		"2026-06-29":                "2026-06-29", // 日付のみ (旧形式) はそのまま
		"":                          "",           // 空は空
		"not-a-date":                "not-a-date", // 解析不能は壊さず素通し
	}
	for in, want := range cases {
		if got := displayDate(in); got != want {
			t.Errorf("displayDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDisplayDateOr(t *testing.T) {
	cases := []struct {
		updated, created, want string
	}{
		// updated あり → updated を使う。
		{"2026-07-06T12:00:00+09:00", "2026-07-01T09:00:00+09:00", "2026-07-06"},
		// updated 空 → created にフォールバック。
		{"", "2026-07-01T09:00:00+09:00", "2026-07-01"},
		// 両方空 → 空 (フォールバック先も空)。
		{"", "", ""},
	}
	for _, c := range cases {
		if got := displayDateOr(c.updated, c.created); got != c.want {
			t.Errorf("displayDateOr(%q, %q) = %q, want %q", c.updated, c.created, got, c.want)
		}
	}
}
