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
