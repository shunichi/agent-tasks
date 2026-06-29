package main

import "strings"

// タスクに紐づく PR (frontmatter の prs: リスト) の表示と検査。
// PR URL は session (着手したエージェントのセッション URL) とは別フィールドに分け、
// 1 タスクに複数 PR (分割 PR / 追従修正) を持てる。記録・移行は skill (done/review) が
// 行い、CLI は読んで show のサマリと doctor の検査で使う。

// prSummary は show の末尾に出す PR 一覧 (複数行) を返す。PR が無ければ ""。
// raw frontmatter にも prs: は出るが、started_at 等と同様に末尾へ見やすく再掲する。
func prSummary(t Task, c colors) string {
	if len(t.PRs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(c.bold + "PR:" + c.reset)
	for _, pr := range t.PRs {
		b.WriteString("\n  " + c.dim + "-" + c.reset + " " + pr)
	}
	return b.String()
}

// PRIssue は prs: の値が PR URL として明らかに不正なものを表す doctor の検出結果。
type PRIssue struct {
	Project string
	ID      string
	Detail  string
	Path    string
}

// findPRIssues は prs: の各値が URL 形式 (http(s)://) かを軽く検査する。
// ホストやパス構造までは縛らない (リポジトリ/ホスティングの違いを許す)。
// 明らかに URL でないもの (session URL の貼り間違いや空白混入) だけ拾う。
func findPRIssues(tasks []Task) []PRIssue {
	var out []PRIssue
	for _, t := range tasks {
		for _, pr := range t.PRs {
			if !strings.HasPrefix(pr, "http://") && !strings.HasPrefix(pr, "https://") {
				out = append(out, PRIssue{t.Project, t.ID, "prs: の値が URL ではない: " + pr, t.Path})
			}
		}
	}
	return out
}
