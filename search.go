package main

import "strings"

// タスクの文字列検索。既定はタイトルの部分一致 (大文字小文字を区別しない)。
// content=true のときは本文 (frontmatter を除いた Markdown) も対象にする。
// list / --json / --recent / report / tui で共有する (list は selectTasks、tui は applyFilter)。

// matchQuery は task が検索クエリ query に一致するかを返す。query が空なら常に true
// (フィルタ無し)。既定はタイトルの部分一致で、content=true なら本文も見る。
// 本文は content 指定時かつタイトル不一致のときだけ読む (無駄な I/O を避ける)。
func matchQuery(t Task, query string, content bool) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	if strings.Contains(strings.ToLower(t.Title), q) {
		return true
	}
	if content && t.Path != "" {
		if strings.Contains(strings.ToLower(taskBody(t.Path)), q) {
			return true
		}
	}
	return false
}
