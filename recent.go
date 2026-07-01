package main

import (
	"cmp"
	"fmt"
	"io"
	"slices"
)

// list --recent N: 最近完了したタスク (done かつ completed_at あり) を completed_at 降順で
// 上位 N 件表示する。直近の成果の振り返り用。completed_at が無い古い done は対象外
// (「完了日時順」が目的なので日時不明は除外する)。スコープ (project / --all-projects) は
// list と同じ既定に従う。

const recentDefaultN = 10

// selectRecent は scope 内の done タスクのうち completed_at が妥当なものを
// completed_at 降順 (同時刻は id 昇順) で最大 n 件返す。
func selectRecent(filterProjects []string, allProjects bool, n int) ([]Task, error) {
	// status=done に絞り、done を隠さない (showAll=true)。スコープは list と共通。アーカイブは対象外。
	rows, _, _, err := selectTasks("done", filterProjects, true, allProjects, false)
	if err != nil {
		return nil, err
	}
	// completed_at が読めないものは除外 (旧データ等)。
	rows = slices.DeleteFunc(rows, func(t Task) bool {
		_, ok := parseTaskTime(t.CompletedAt)
		return !ok
	})
	slices.SortFunc(rows, func(a, b Task) int {
		ta, _ := parseTaskTime(a.CompletedAt)
		tb, _ := parseTaskTime(b.CompletedAt)
		return cmp.Or(tb.Compare(ta), cmp.Compare(a.ID, b.ID)) // completed_at 降順、同時刻は id 昇順
	})
	if n >= 0 && len(rows) > n {
		rows = rows[:n]
	}
	return rows, nil
}

// runRecentTable は最近完了タスクを COMPLETED 列付きのテーブルで描画する
// (通常 list の UPDATED の代わりに、完了日時を主役にする)。
func runRecentTable(w io.Writer, rows []Task) {
	c := newColors()
	if len(rows) == 0 {
		fmt.Fprintln(w, "完了タスクなし (completed_at が記録されたもの)")
		return
	}
	tbl := newTable("PROJECT", "ID", "COMPLETED", "TITLE")
	for _, t := range rows {
		tbl.add(
			cell{t.Project, c.dim},
			cell{t.ID, ""},
			cell{displayDate(t.CompletedAt), c.dim},
			cell{t.Title, ""},
		)
	}
	tbl.render(w, c)
	fmt.Fprintf(w, "\n%s最近完了 %d 件%s\n", c.dim, len(rows), c.reset)
}
