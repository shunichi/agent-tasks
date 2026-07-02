package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// worktime は「そのタスクに実際に手が動いていた時間」を集計する。着手〜完了の壁時計
// (リードタイム) ではなく、hook が記録する working/waiting 遷移ログ (worktime/<session_id>.jsonl)
// から working だった区間だけを合計する (ユーザーの入力待ち = waiting は除く)。
//
// タスクへの帰属: タスクは session-link で session_id に紐づく。1 セッションで複数タスクを直列に
// こなす (batch / 同一セッションで順に start) と 1 つのログに複数タスクの遷移が混ざるので、
// タスクの [started_at, completed_at] 窓でクリップして切り出す (タスクは 1 セッション内で時間的に
// 重ならないため窓で分離できる)。spawn の子セッションはそのタスク専用なので窓 = セッション全体。
//
// Phase 1 の制限: 帰属は「現在 link されている 1 セッション」を使う。再 start で別セッションに
// 移ると link が上書きされ、前セッション分は集計されない (可視化 Web アプリと合わせて後で拡張)。

// timeInterval は [Start, End) の時間区間。
type timeInterval struct {
	Start time.Time
	End   time.Time
}

// workingIntervals は状態遷移イベント (時刻昇順) から working だった区間を復元する。
// working イベントで区間を開き、次のイベント (waiting/ended 等) で閉じる。最後が working のまま
// 閉じられていなければ openEnd で閉じる (セッション継続中や SessionEnd 未達のとき)。
func workingIntervals(events []worktimeEvent, openEnd time.Time) []timeInterval {
	var out []timeInterval
	var curStart time.Time
	inWorking := false
	closeAt := func(t time.Time) {
		if inWorking && t.After(curStart) {
			out = append(out, timeInterval{curStart, t})
		}
		inWorking = false
	}
	for _, e := range events {
		t := parseSessionTime(e.Ts)
		if t.IsZero() {
			continue
		}
		if e.State == sessWorking {
			if !inWorking {
				curStart, inWorking = t, true
			}
			continue
		}
		closeAt(t) // waiting / ended / その他は working を閉じる
	}
	closeAt(openEnd)
	return out
}

// clipIntervals は各区間を [winStart, winEnd] と交差させ、空でないものだけ返す。
func clipIntervals(ivs []timeInterval, winStart, winEnd time.Time) []timeInterval {
	var out []timeInterval
	for _, iv := range ivs {
		s, e := iv.Start, iv.End
		if s.Before(winStart) {
			s = winStart
		}
		if e.After(winEnd) {
			e = winEnd
		}
		if e.After(s) {
			out = append(out, timeInterval{s, e})
		}
	}
	return out
}

// sumIntervals は区間の合計を返す。
func sumIntervals(ivs []timeInterval) time.Duration {
	var total time.Duration
	for _, iv := range ivs {
		total += iv.End.Sub(iv.Start)
	}
	return total
}

// taskWorktime はタスクの実稼働区間 (窓クリップ済み) と合計を求める。ok=false は link が無く
// セッションを特定できないとき (hook 未導入 / start が session-link を書いていない)。
func taskWorktime(t Task, now time.Time) (ivs []timeInterval, total time.Duration, sessionID string, ok bool, err error) {
	key := taskSessionKey(t)
	if key == "" {
		return nil, 0, "", false, nil
	}
	link, has := readSessionLink(key)
	if !has {
		return nil, 0, "", false, nil
	}
	events, err := readWorktimeEvents(link.SessionID)
	if err != nil {
		return nil, 0, link.SessionID, true, err
	}
	// 窓: [started_at, completed_at]。completed_at が無ければ now まで (稼働中)。
	winStart, okStart := parseTaskTime(t.StartedAt)
	if !okStart {
		winStart = time.Time{} // started_at 無し: 下限なし
	}
	winEnd := now
	if ct, okEnd := parseTaskTime(t.CompletedAt); okEnd {
		winEnd = ct
	}
	ivs = clipIntervals(workingIntervals(events, winEnd), winStart, winEnd)
	return ivs, sumIntervals(ivs), link.SessionID, true, nil
}

// cmdWorktime は `worktime [<project>] <id> [--json]`。タスクの実稼働時間と稼働区間を表示する。
func cmdWorktime(args []string) error {
	jsonOut := false
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch a {
		case "--json":
			jsonOut = true
		default:
			s.positional(a)
		}
	}
	project, id, err := resolveProjectID(s.rest())
	if err != nil {
		return err
	}
	path, err := resolveTaskPath(project, id)
	if err != nil {
		return err
	}
	t, err := parseTask(path)
	if err != nil {
		return err
	}
	now := time.Now()
	ivs, total, sessionID, ok, err := taskWorktime(t, now)
	if err != nil {
		return err
	}

	if jsonOut {
		return printWorktimeJSON(t, ivs, total, sessionID, ok)
	}
	return printWorktimeHuman(t, ivs, total, sessionID, ok, now)
}

func printWorktimeHuman(t Task, ivs []timeInterval, total time.Duration, sessionID string, ok bool, now time.Time) error {
	fmt.Printf("%s/%s  %s\n", t.Project, normalizeID(t.ID), t.Title)
	lead := leadTime(t.StartedAt, t.CompletedAt)
	if lead != "" {
		fmt.Printf("  リードタイム (壁時計): %s\n", lead)
	}
	if !ok {
		fmt.Println("  実稼働: セッションが紐づいていません (session-link 未実行 / hook 未導入)。")
		return nil
	}
	fmt.Printf("  実稼働 (working 合計): %s  (%d 区間)\n", humanizeDuration(total), len(ivs))
	if len(ivs) == 0 {
		fmt.Println("  稼働区間: 記録なし (この機能の導入後に着手したタスクから記録されます)")
		return nil
	}
	fmt.Println("  稼働区間:")
	for _, iv := range ivs {
		fmt.Printf("    %s %s–%s  (%s)\n",
			iv.Start.Format("01-02"), iv.Start.Format("15:04"), iv.End.Format("15:04"),
			humanizeDuration(iv.End.Sub(iv.Start)))
	}
	return nil
}

// worktimeJSON は --json / 可視化 Web アプリの入力になる形。
type worktimeJSON struct {
	Project        string                 `json:"project"`
	ID             string                 `json:"id"`
	Title          string                 `json:"title"`
	SessionID      string                 `json:"session_id,omitempty"`
	StartedAt      string                 `json:"started_at,omitempty"`
	CompletedAt    string                 `json:"completed_at,omitempty"`
	Linked         bool                   `json:"linked"` // セッションが紐づいているか
	WorkingSeconds int64                  `json:"working_seconds"`
	WorkingHuman   string                 `json:"working_human"`
	Intervals      []worktimeIntervalJSON `json:"intervals"`
}

type worktimeIntervalJSON struct {
	Start   string `json:"start"` // RFC3339
	End     string `json:"end"`   // RFC3339
	Seconds int64  `json:"seconds"`
}

func printWorktimeJSON(t Task, ivs []timeInterval, total time.Duration, sessionID string, ok bool) error {
	out := worktimeJSON{
		Project:        t.Project,
		ID:             normalizeID(t.ID),
		Title:          t.Title,
		SessionID:      sessionID,
		StartedAt:      t.StartedAt,
		CompletedAt:    t.CompletedAt,
		Linked:         ok,
		WorkingSeconds: int64(total.Seconds()),
		WorkingHuman:   humanizeDuration(total),
		Intervals:      make([]worktimeIntervalJSON, 0, len(ivs)),
	}
	for _, iv := range ivs {
		out.Intervals = append(out.Intervals, worktimeIntervalJSON{
			Start:   iv.Start.Format(time.RFC3339),
			End:     iv.End.Format(time.RFC3339),
			Seconds: int64(iv.End.Sub(iv.Start).Seconds()),
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
