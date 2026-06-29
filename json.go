package main

import (
	"encoding/json"
	"io"
	"time"
)

// list / show の --json 出力。人間向けのテーブル (色付け・CJK 幅揃え・title 切り詰め・
// 日時の丸め) ではなく、skill / スクリプトが扱える「計算済みの正準ビュー」を出す。
// 時刻は ISO8601 のまま (丸めない)、色・幅揃えは無し。

// taskJSON は 1 タスクの機械可読表現。frontmatter の生値に、CLI が計算する付加情報
// (session_state / blocked_for) を加えたもの。空のフィールドは omitempty で落とす
// (id/project/title/status/created/updated は常に出す)。
type taskJSON struct {
	ID            string   `json:"id"`
	Project       string   `json:"project"`
	Title         string   `json:"title"`
	Status        string   `json:"status"`
	Agent         string   `json:"agent,omitempty"`
	Session       string   `json:"session,omitempty"`
	Branch        string   `json:"branch,omitempty"`
	Worktree      string   `json:"worktree,omitempty"`
	Created       string   `json:"created"`
	Updated       string   `json:"updated"`
	StartedAt     string   `json:"started_at,omitempty"`
	CompletedAt   string   `json:"completed_at,omitempty"`
	BlockedAt     string   `json:"blocked_at,omitempty"`
	BlockedReason string   `json:"blocked_reason,omitempty"`
	PRs           []string `json:"prs,omitempty"`

	// 計算済みフィールド (frontmatter には無い):
	SessionState string `json:"session_state,omitempty"` // in-progress のみ: working|waiting|ended|unknown
	BlockedFor   string `json:"blocked_for,omitempty"`   // blocked のみ: blocked_at からの経過 (例 "3d")
}

// toTaskJSON は Task を機械可読表現に変換する。session_state は in-progress のときだけ
// (マーカーが無ければ "unknown")、blocked_for は blocked かつ blocked_at が妥当なときだけ付ける。
func toTaskJSON(t Task, now time.Time) taskJSON {
	j := taskJSON{
		ID: t.ID, Project: t.Project, Title: t.Title, Status: t.Status,
		Agent: t.Agent, Session: t.Session, Branch: t.Branch, Worktree: t.Worktree,
		Created: t.Created, Updated: t.Updated,
		StartedAt: t.StartedAt, CompletedAt: t.CompletedAt,
		BlockedAt: t.BlockedAt, BlockedReason: t.BlockedReason,
		PRs: t.PRs,
	}
	if t.Status == "in-progress" {
		if st, ok := taskSessionState(t); ok {
			j.SessionState = st.State
		} else {
			j.SessionState = "unknown"
		}
	}
	if t.Status == "blocked" && t.BlockedAt != "" {
		if _, ok := parseTaskTime(t.BlockedAt); ok {
			j.BlockedFor = humanizeSince(t.BlockedAt, now)
		}
	}
	return j
}

// newJSONEncoder は両出力で共通のエンコーダ設定 (インデント / HTML エスケープ無効) を返す。
// SetEscapeHTML(false) で title 内の < > & をそのまま出す (機械可読・可読性優先)。
func newJSONEncoder(w io.Writer) *json.Encoder {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc
}

// writeTasksJSON は rows を taskJSON の配列として出力する (該当無しは []）。
func writeTasksJSON(w io.Writer, rows []Task, now time.Time) error {
	out := make([]taskJSON, len(rows))
	for i, t := range rows {
		out[i] = toTaskJSON(t, now)
	}
	return newJSONEncoder(w).Encode(out)
}

// writeTaskJSON は 1 タスクを taskJSON オブジェクトとして出力する (show --json 用)。
func writeTaskJSON(w io.Writer, t Task, now time.Time) error {
	return newJSONEncoder(w).Encode(toTaskJSON(t, now))
}
