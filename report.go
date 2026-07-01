package main

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"
)

// report は一定期間 (月/週/任意区間) に完了したタスクを markdown で出力する。
// 対象は done かつ completed_at がその期間内のタスク。スコープは list と同じ規則
// (既定は現在 project、--all-projects で横断、--project で指定)。横断時は project ごとに
// セクション分けする。所要時間 (started_at → completed_at) と合計/平均のサマリを添える。

func cmdReport(args []string) error {
	var monthVal, weekVal, sinceVal, untilVal string
	var filterProjects []string
	allProjects := false
	monthSet, weekSet := false, false
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch a {
		case "--project":
			v, err := s.value("--project")
			if err != nil {
				return err
			}
			filterProjects = append(filterProjects, v)
		case "--projects":
			v, err := s.value("--projects")
			if err != nil {
				return err
			}
			filterProjects = append(filterProjects, splitProjects(v)...)
		case "--all-projects":
			allProjects = true
		case "--month":
			// 任意で YYYY-MM を取る (次が値なら消費、無ければ今月)。
			monthSet = true
			if v, ok := s.peek(); ok && !strings.HasPrefix(v, "-") {
				s.skip()
				monthVal = v
			}
		case "--week":
			// 任意で YYYY-MM-DD を取る (その日を含む週。無ければ今週)。
			weekSet = true
			if v, ok := s.peek(); ok && !strings.HasPrefix(v, "-") {
				s.skip()
				weekVal = v
			}
		case "--since":
			v, err := s.value("--since")
			if err != nil {
				return err
			}
			sinceVal = v
		case "--until":
			v, err := s.value("--until")
			if err != nil {
				return err
			}
			untilVal = v
		default:
			return usagef("unknown option: %s", a)
		}
	}
	if pos := s.rest(); len(pos) > 0 {
		return usagef("unexpected argument: %s", pos[0])
	}

	now := time.Now()
	since, until, label, err := resolveReportPeriod(now, monthSet, monthVal, weekSet, weekVal, sinceVal, untilVal)
	if err != nil {
		return err
	}

	rows, effProjects, _, err := selectTasks("done", filterProjects, true, allProjects, false, "", false)
	if err != nil {
		return err
	}
	// completed_at が [since, until) に入る done タスクだけを対象にする。
	var inRange []Task
	for _, t := range rows {
		ct, ok := parseTaskTime(t.CompletedAt)
		if !ok {
			continue // completed_at が無い/壊れている旧データは対象外
		}
		if !ct.Before(since) && ct.Before(until) {
			inRange = append(inRange, t)
		}
	}
	slices.SortFunc(inRange, func(a, b Task) int {
		ta, _ := parseTaskTime(a.CompletedAt)
		tb, _ := parseTaskTime(b.CompletedAt)
		return cmp.Or(
			cmp.Compare(a.Project, b.Project),
			ta.Compare(tb), // 完了日時 昇順 (時系列)
			compareID(a.ID, b.ID),
		)
	})

	writeReport(os.Stdout, inRange, label, len(effProjects) == 0)
	return nil
}

// resolveReportPeriod は期間指定フラグから [since, until) (until 排他) と表示ラベルを決める。
// 優先順位: --since/--until > --week > --month (既定は今月)。日付は YYYY-MM(-DD) をローカルで解釈。
func resolveReportPeriod(now time.Time, monthSet bool, monthVal string, weekSet bool, weekVal, sinceVal, untilVal string) (since, until time.Time, label string, err error) {
	loc := now.Location()

	// 任意区間: --since / --until (どちらか一方でも可)。until 指定はその日を含む (翌日 0 時を排他境界に)。
	if sinceVal != "" || untilVal != "" {
		until = now
		sinceLabel, untilLabel := "最古", now.Format("2006-01-02")
		if sinceVal != "" {
			d, e := time.ParseInLocation("2006-01-02", sinceVal, loc)
			if e != nil {
				return since, until, "", usagef("--since は YYYY-MM-DD 形式です: %q", sinceVal)
			}
			since = d
			sinceLabel = d.Format("2006-01-02")
		}
		if untilVal != "" {
			d, e := time.ParseInLocation("2006-01-02", untilVal, loc)
			if e != nil {
				return since, until, "", usagef("--until は YYYY-MM-DD 形式です: %q", untilVal)
			}
			until = d.AddDate(0, 0, 1)
			untilLabel = d.Format("2006-01-02")
		}
		return since, until, sinceLabel + " 〜 " + untilLabel, nil
	}

	// 週: --week [YYYY-MM-DD]。その日 (無ければ今日) を含む月曜〜日曜。
	if weekSet {
		base := now
		if weekVal != "" {
			d, e := time.ParseInLocation("2006-01-02", weekVal, loc)
			if e != nil {
				return since, until, "", usagef("--week は YYYY-MM-DD 形式です: %q", weekVal)
			}
			base = d
		}
		day := time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, loc)
		offset := (int(day.Weekday()) + 6) % 7 // 月曜=0 になるよう調整
		since = day.AddDate(0, 0, -offset)
		until = since.AddDate(0, 0, 7)
		label = "週 " + since.Format("2006-01-02") + " 〜 " + until.AddDate(0, 0, -1).Format("2006-01-02")
		return since, until, label, nil
	}

	// 月: --month [YYYY-MM] (既定は今月)。
	base := now
	if monthVal != "" {
		d, e := time.ParseInLocation("2006-01", monthVal, loc)
		if e != nil {
			return since, until, "", usagef("--month は YYYY-MM 形式です: %q", monthVal)
		}
		base = d
	}
	since = time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, loc)
	until = since.AddDate(0, 1, 0)
	return since, until, since.Format("2006-01"), nil
}

// writeReport は対象タスクを markdown で出力する。cross=true (横断) のときは project ごとに
// セクション分けし、最後に全体のサマリ (合計件数 / 所要合計・平均) を添える。
// rows は (project, completed_at 昇順) でソート済み前提。
func writeReport(w io.Writer, rows []Task, periodLabel string, cross bool) {
	fmt.Fprintf(w, "# 完了レポート: %s\n\n", periodLabel)
	if len(rows) == 0 {
		fmt.Fprintln(w, "対象期間に完了したタスクはありません。")
		return
	}

	var totalLead time.Duration
	var leadCount int
	for i := 0; i < len(rows); {
		proj := rows[i].Project
		if cross {
			fmt.Fprintf(w, "## %s\n\n", proj)
		}
		fmt.Fprintln(w, "| ID | タイトル | 開始 | 完了 | 所要 |")
		fmt.Fprintln(w, "| --- | --- | --- | --- | --- |")
		n := 0
		for i < len(rows) && rows[i].Project == proj {
			t := rows[i]
			lt := leadTime(t.StartedAt, t.CompletedAt)
			if lt == "" {
				lt = "-" // started_at 欠けの旧データ等
			}
			fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n",
				t.ID, mdEscape(t.Title), reportTime(t.StartedAt), reportTime(t.CompletedAt), lt)
			if s, okS := parseTaskTime(t.StartedAt); okS {
				if c, okC := parseTaskTime(t.CompletedAt); okC {
					totalLead += c.Sub(s)
					leadCount++
				}
			}
			n++
			i++
		}
		fmt.Fprintf(w, "\n%d 件\n\n", n)
	}

	fmt.Fprintf(w, "---\n\n合計 %d 件", len(rows))
	if leadCount > 0 {
		fmt.Fprintf(w, " / 所要合計 %s / 平均 %s (所要を算出できた %d 件)",
			humanizeDuration(totalLead), humanizeDuration(totalLead/time.Duration(leadCount)), leadCount)
	}
	fmt.Fprintln(w)
}

// reportTime は時刻文字列を "YYYY-MM-DD HH:MM" に整形する。パースできなければ "-"。
func reportTime(s string) string {
	tm, ok := parseTaskTime(s)
	if !ok {
		return "-"
	}
	return tm.Format("2006-01-02 15:04")
}

// mdEscape は markdown 表のセルに入れても崩れないよう "|" と改行を無害化する。
func mdEscape(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "\n", " ")
}
