package main

import (
	"fmt"
	"hash/fnv"
	"html/template"
	"io"
	"slices"
	"time"
)

// worktime のタイムライン可視化 (serve の /worktime ルート)。稼働区間 (0102 で全セッション合算) を
// 日 × 時刻 (0–24h) の帯で俯瞰する。serve と同じく外部依存なしの自己完結 HTML (インライン CSS)。

// wtBlock は 1 日の中の 1 稼働ブロック (24h 軸上の位置と幅、%)。
type wtBlock struct {
	LeftPct  float64
	WidthPct float64
	ColorIdx int
	Label    string // ホバー表示 "#ID title  HH:MM–HH:MM"
}

// wtDay は 1 日分の行。
type wtDay struct {
	Date   string // "2026-07-02 (水)"
	Total  string // その日の実稼働合計
	Blocks []wtBlock
}

// wtLegend は凡例の 1 タスク (色 + 合計)。
type wtLegend struct {
	Project  string
	ID       string
	Title    string
	Total    string
	ColorIdx int
}

type timelineData struct {
	Days     []wtDay
	Legend   []wtLegend
	ColorCSS template.CSS // 色クラス (.wt-c<idx>{...}) をまとめて注入
	GrandTot string
	Now      string
	Interval int
	Refresh  bool
	Axis     []axisMark // 時刻軸の目盛 (0,3,...,24)
}

// axisMark は時刻軸の 1 目盛 (ラベルと左位置 %)。
type axisMark struct {
	Label   int
	LeftPct float64
}

// taskColorHue はタスク (project/id) から決定的な色相 (0–359) を返す。
func taskColorHue(project, id string) int {
	h := fnv.New32a()
	h.Write([]byte(project + "/" + id))
	return int(h.Sum32() % 360)
}

var weekdayJa = [...]string{"日", "月", "火", "水", "木", "金", "土"}

// buildTimeline は横断集計 (collectWorktimes) からタイムライン表示データを組む。
// 稼働区間を日境界で分割し、各日を 0–24h の帯に配置する。日は新しい順。
func buildTimeline(results []taskWorktimeResult, interval int, now time.Time) timelineData {
	// タスクごとに色 index を割り当て、凡例と ColorCSS を作る。
	idxOf := map[string]int{}
	var css string
	legend := make([]wtLegend, 0, len(results))
	var grand time.Duration
	for _, r := range results {
		key := r.Project + "/" + r.ID
		if _, ok := idxOf[key]; !ok {
			idx := len(idxOf)
			idxOf[key] = idx
			hue := taskColorHue(r.Project, r.ID)
			css += fmt.Sprintf(".wt-c%d{background:hsl(%d,60%%,55%%)}", idx, hue)
		}
		grand += r.Total
		legend = append(legend, wtLegend{
			Project: r.Project, ID: r.ID, Title: r.Title,
			Total: humanizeDuration(r.Total), ColorIdx: idxOf[key],
		})
	}

	type dayAcc struct {
		total  time.Duration
		blocks []wtBlock
	}
	days := map[string]*dayAcc{}
	var order []string
	for _, r := range results {
		idx := idxOf[r.Project+"/"+r.ID]
		for _, iv := range r.Intervals {
			cur := iv.Start
			for cur.Before(iv.End) {
				dayStart := time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, cur.Location())
				nextDay := dayStart.AddDate(0, 0, 1)
				pieceEnd := iv.End
				if pieceEnd.After(nextDay) {
					pieceEnd = nextDay
				}
				key := dayStart.Format("2006-01-02")
				acc := days[key]
				if acc == nil {
					acc = &dayAcc{}
					days[key] = acc
					order = append(order, key)
				}
				acc.total += pieceEnd.Sub(cur)
				acc.blocks = append(acc.blocks, wtBlock{
					LeftPct:  cur.Sub(dayStart).Minutes() / 1440 * 100,
					WidthPct: pieceEnd.Sub(cur).Minutes() / 1440 * 100,
					ColorIdx: idx,
					Label:    fmt.Sprintf("#%s %s  %s–%s", r.ID, r.Title, cur.Format("15:04"), pieceEnd.Format("15:04")),
				})
				cur = pieceEnd
			}
		}
	}
	slices.Sort(order)
	slices.Reverse(order) // 新しい日を上に
	dayList := make([]wtDay, 0, len(order))
	for _, k := range order {
		acc := days[k]
		d, _ := time.Parse("2006-01-02", k)
		dayList = append(dayList, wtDay{
			Date:   k + " (" + weekdayJa[d.Weekday()] + ")",
			Total:  humanizeDuration(acc.total),
			Blocks: acc.blocks,
		})
	}

	return timelineData{
		Days:     dayList,
		Legend:   legend,
		ColorCSS: template.CSS(css), //nolint:gosec // hsl() のみを自前生成
		GrandTot: humanizeDuration(grand),
		Now:      now.Format("2006-01-02 15:04:05"),
		Interval: interval,
		Refresh:  interval > 0,
		Axis:     hourAxis(),
	}
}

// hourAxis は 3 時間おきの時刻目盛 (0–24) を左位置 % 付きで返す。
func hourAxis() []axisMark {
	var out []axisMark
	for h := 0; h <= 24; h += 3 {
		out = append(out, axisMark{Label: h, LeftPct: float64(h) / 24 * 100})
	}
	return out
}

func renderTimeline(w io.Writer, results []taskWorktimeResult, interval int, now time.Time) error {
	return timelineTemplate.Execute(w, buildTimeline(results, interval, now))
}

var timelineTemplate = template.Must(template.New("timeline").Parse(timelineHTML))

const timelineHTML = `<!doctype html>
<html lang="ja">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
{{if .Refresh}}<meta http-equiv="refresh" content="{{.Interval}}">{{end}}
<title>agent-tasks — 稼働時間</title>
<style>
  :root {
    --bg: #0f1115; --panel: #171a21; --card: #1d222b; --border: #2a2f3a;
    --fg: #e6e8ec; --dim: #8b93a1; --accent: #4a9eff;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 0 0 2rem; background: var(--bg); color: var(--fg);
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Hiragino Sans", "Noto Sans JP", sans-serif;
    font-size: 15px; line-height: 1.5;
  }
  header {
    position: sticky; top: 0; z-index: 10; background: var(--panel);
    border-bottom: 1px solid var(--border); padding: 0.7rem 1rem;
  }
  header h1 { margin: 0; font-size: 1.1rem; display: flex; align-items: center; gap: 0.6rem; }
  header h1 a { color: var(--accent); text-decoration: none; font-size: 0.85rem; font-weight: 400; }
  header .meta { color: var(--dim); font-size: 0.8rem; margin-top: 0.15rem; }
  main { padding: 0.75rem; }
  .axis { position: relative; height: 1.1rem; margin: 0 0 0.2rem; margin-left: 6.5rem; color: var(--dim); font-size: 0.7rem; }
  .axis span { position: absolute; transform: translateX(-50%); }
  .day { display: flex; align-items: stretch; gap: 0.4rem; margin-bottom: 0.35rem; }
  .day .lbl { width: 6.1rem; flex: none; font-size: 0.78rem; }
  .day .lbl .d { font-variant-numeric: tabular-nums; }
  .day .lbl .t { color: var(--dim); display: block; font-size: 0.72rem; }
  .track {
    position: relative; flex: 1; height: 1.6rem; background: var(--card);
    border: 1px solid var(--border); border-radius: 4px; overflow: hidden;
    background-image: repeating-linear-gradient(to right, transparent, transparent calc(100%/24 - 1px), var(--border) calc(100%/24 - 1px), var(--border) calc(100%/24));
  }
  .blk { position: absolute; top: 2px; bottom: 2px; min-width: 2px; border-radius: 2px; opacity: 0.9; }
  {{.ColorCSS}}
  .legend { margin-top: 1.2rem; }
  .legend h2 { font-size: 0.9rem; margin: 0 0 0.5rem; }
  .lg { display: flex; align-items: center; gap: 0.5rem; padding: 0.15rem 0; font-size: 0.82rem; }
  .lg .sw { width: 0.9rem; height: 0.9rem; border-radius: 3px; flex: none; }
  .lg .lgid { color: #ffd479; font-weight: 700; font-variant-numeric: tabular-nums; }
  .lg .lgt { color: var(--dim); }
  .lg .lgttl { word-break: break-word; }
  .empty { color: var(--dim); text-align: center; margin-top: 3rem; }
</style>
</head>
<body>
<header>
  <h1>稼働時間 <a href="/">← 一覧</a></h1>
  <div class="meta">合計 {{.GrandTot}} · {{.Now}}{{if .Refresh}} · 自動更新 {{.Interval}}s{{end}}</div>
</header>
<main>
{{if .Days}}
  <div class="axis">
    {{range .Axis}}<span style="left:{{printf "%.2f" .LeftPct}}%">{{.Label}}</span>{{end}}
  </div>
  {{range .Days}}
  <div class="day">
    <div class="lbl"><span class="d">{{.Date}}</span><span class="t">{{.Total}}</span></div>
    <div class="track">
      {{range .Blocks}}<div class="blk wt-c{{.ColorIdx}}" style="left:{{printf "%.2f" .LeftPct}}%;width:{{printf "%.2f" .WidthPct}}%" title="{{.Label}}"></div>{{end}}
    </div>
  </div>
  {{end}}
  <div class="legend">
    <h2>タスク ({{len .Legend}})</h2>
    {{range .Legend}}
    <div class="lg">
      <span class="sw wt-c{{.ColorIdx}}"></span>
      <span class="lgid">#{{.ID}}</span>
      <span class="lgt">{{.Project}} · {{.Total}}</span>
      <span class="lgttl">{{.Title}}</span>
    </div>
    {{end}}
  </div>
{{else}}
  <p class="empty">稼働記録がまだありません (この機能の導入後に着手したタスクから記録されます)</p>
{{end}}
</main>
</body>
</html>
`
