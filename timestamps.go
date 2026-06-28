package main

import (
	"fmt"
	"strings"
	"time"
)

// タスクの着手/完了日時 (started_at / completed_at) の表示と整合チェック。
// 記録は skill (start で started_at、done で completed_at) が行い、CLI は読んで
// show のサマリや doctor の検査に使う。日時のパース/整形は datetime.go の共通ヘルパを使う。

// timestampSummary は show の末尾に出す「着手/完了/所要時間 (または経過)」の要約行を返す。
// started_at / completed_at がどちらも無ければ "" (footer を出さない)。
func timestampSummary(t Task, now time.Time, c colors) string {
	started, okS := parseTaskTime(t.StartedAt)
	completed, okC := parseTaskTime(t.CompletedAt)
	if !okS && !okC {
		return ""
	}
	const layout = "2006-01-02 15:04"
	var parts []string
	if okS {
		parts = append(parts, "着手 "+started.Format(layout))
	}
	switch {
	case okS && okC:
		// 完了済み: 完了日時 + リードタイム (着手→完了)。
		parts = append(parts, fmt.Sprintf("完了 %s (リードタイム %s)",
			completed.Format(layout), humanizeDuration(completed.Sub(started))))
	case okC:
		// started_at 欠落で completed_at だけある (整合性は doctor が拾う)。
		parts = append(parts, "完了 "+completed.Format(layout))
	case okS && t.Status == "in-progress":
		// 進行中: 着手からの経過。
		parts = append(parts, fmt.Sprintf("経過 %s", humanizeDuration(now.Sub(started))))
	}
	return c.dim + strings.Join(parts, "  ") + c.reset
}

// TimestampIssue は status と着手/完了日時の食い違いを表す doctor の検出結果。
type TimestampIssue struct {
	Project string
	ID      string
	Detail  string
	Path    string
}

// findTimestampIssues は started_at / completed_at の論理的な矛盾を拾う。
// **純粋な欠落は問題にしない** (この機能より前のタスクは元々どちらも持たないため)。
// 拾うのは「あり得ない組み合わせ」と「新しい記録漏れ」:
//   - completed_at はあるが started_at が無い (done の記録漏れ/順序おかしい)
//   - completed_at が started_at より前 (時系列の矛盾)
//   - status=done かつ started_at はあるのに completed_at が無い (done の記録漏れ)。
//     started_at を持つ = 本機能以降のタスクなので、旧 done を誤検出しない。
func findTimestampIssues(tasks []Task) []TimestampIssue {
	var out []TimestampIssue
	for _, t := range tasks {
		started, okS := parseTaskTime(t.StartedAt)
		completed, okC := parseTaskTime(t.CompletedAt)
		switch {
		case okC && !okS:
			out = append(out, TimestampIssue{t.Project, t.ID, "completed_at があるのに started_at が無い", t.Path})
		case okC && okS && completed.Before(started):
			out = append(out, TimestampIssue{t.Project, t.ID, "completed_at が started_at より前", t.Path})
		case t.Status == "done" && okS && !okC:
			out = append(out, TimestampIssue{t.Project, t.ID, "status=done なのに completed_at が無い (記録漏れ)", t.Path})
		}
	}
	return out
}
