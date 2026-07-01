package main

import "strings"

// タスクに紐づく外部 issue tracker / 課題管理の URL (frontmatter の tracker: リスト) の表示と検査。
// prs: (PR 専用) とは別枠で、任意ホストの関連 URL を汎用に保持する (特定サービス非依存)。
// 1 タスクに複数持てる。記録は skill (タスク登録/done 手順) が行い、CLI は読んで show のサマリと
// doctor の検査で使う。

// trackerSummary は show の末尾に出す関連 URL 一覧 (複数行) を返す。無ければ ""。
func trackerSummary(t Task, c colors) string {
	if len(t.Tracker) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(c.bold + "tracker:" + c.reset)
	for _, u := range t.Tracker {
		b.WriteString("\n  " + c.dim + "-" + c.reset + " " + u)
	}
	return b.String()
}

// TrackerProblem は tracker: の値が URL として明らかに不正なものを表す doctor の検出結果。
type TrackerProblem struct {
	Project string
	ID      string
	Detail  string
	Path    string
}

// findTrackerProblems は tracker: の各値が URL 形式 (http(s)://) かを軽く検査する。
// ホストやパス構造は縛らない (任意の tracker を許す)。明らかに URL でないものだけ拾う。
func findTrackerProblems(tasks []Task) []TrackerProblem {
	var out []TrackerProblem
	for _, t := range tasks {
		for _, u := range t.Tracker {
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				out = append(out, TrackerProblem{t.Project, t.ID, "tracker: の値が URL ではない: " + u, t.Path})
			}
		}
	}
	return out
}
