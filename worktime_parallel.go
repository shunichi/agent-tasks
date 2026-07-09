package main

import (
	"cmp"
	"embed"
	"encoding/json"
	"html/template"
	"io"
	"slices"
	"strings"
	"time"
)

// worktime の「時間帯別の並列稼働」ビュー (0127。/worktime?view=parallel)。0104 の時間配分ビュー
// (量に集中) とは別ビューで、こちらは **時刻 × 並列度** に集中する: 1日のどの時間帯に、どれだけ
// 並列で作業が走っていたかを俯瞰する。稼働は短バーストの集まりで絶対時刻の色帯はスマホで針化する
// ため、**PC (広い画面) 限定**とし、横に広い 0–24h 軸を前提にする (狭幅では案内を出す)。
//
// 構成は 3 段のドリルダウン:
//   - 俯瞰: 曜日 × 時刻ヒートマップ (「典型的な1週間」の平均並列度)
//   - 一覧: 日別スイムレーン (各日 0–24h に稼働区間を重ね描き。重なり = 並列度)
//   - 詳細: 日をクリック → その1日をタスク別レーン + 並列度ストリップに展開
//
// サーバは稼働区間を **日境界で分割した piece** (project/id/title 付き) として JSON 埋め込みするだけ。
// 並列度 (15分ビンの同時本数)・ヒートマップ・レーン配置の集計はクライアント JS が行う (既存
// 時間配分ビューと同じ「サーバは生データ、集計は JS」方針)。日次集計の wtEntry ではなく生区間を使う
// (詳細でタスク別に割るため id/title が要る)。

// parallelPiece は 1 区間を「1 日ぶん」に分割したもの (日をまたぐ区間は複数 piece になる)。
// Start/End は当日 0:00 からの分 (0..1440)。クライアントは Date でグルーピングし、分で時刻軸へ置く。
type parallelPiece struct {
	Project string `json:"p"`
	ID      string `json:"id"`
	Title   string `json:"ti"`
	Date    string `json:"d"` // ローカル日付 "2006-01-02"
	Start   int    `json:"s"` // 当日 0:00 からの分 (0..1440)
	End     int    `json:"e"` // 同上 (Start < End)
}

// buildParallelPieces は各タスクの稼働区間を日境界で分割し、当日分単位の piece に落とす。
// 出力は日の新しい順 → 開始分 → id の決定的順序 (buildWorktimeEntries と同じ日分割の考え方)。
func buildParallelPieces(results []taskWorktimeResult) []parallelPiece {
	var out []parallelPiece
	for _, r := range results {
		for _, iv := range r.Intervals {
			cur := iv.Start
			for cur.Before(iv.End) {
				dayStart := time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, cur.Location())
				nextDay := dayStart.AddDate(0, 0, 1)
				pieceEnd := iv.End
				if pieceEnd.After(nextDay) {
					pieceEnd = nextDay
				}
				startMin := int(cur.Sub(dayStart).Minutes())
				endMin := int(pieceEnd.Sub(dayStart).Minutes())
				if endMin > startMin {
					out = append(out, parallelPiece{
						Project: r.Project, ID: r.ID, Title: r.Title,
						Date: dayStart.Format("2006-01-02"), Start: startMin, End: endMin,
					})
				}
				cur = pieceEnd
			}
		}
	}
	slices.SortFunc(out, func(a, b parallelPiece) int {
		return cmp.Or(strings.Compare(b.Date, a.Date), cmp.Compare(a.Start, b.Start), strings.Compare(a.ID, b.ID))
	})
	return out
}

// parallelColors は結果に現れる project に色を割り当てる (時間配分ビューと同じ順序ロジックを共有し、
// 両ビューで色が一致する)。
func parallelColors(results []taskWorktimeResult) map[string]string {
	seen := map[string]bool{}
	var names []string
	for _, r := range results {
		if !seen[r.Project] {
			seen[r.Project] = true
			names = append(names, r.Project)
		}
	}
	return assignProjectColors(names)
}

// parallelJSONData は /worktime?view=parallel&format=json のレスポンス (自動更新ポーリング用)。
type parallelJSONData struct {
	Pieces []parallelPiece   `json:"p"`
	Colors map[string]string `json:"c"`
}

func renderParallelJSON(w io.Writer, results []taskWorktimeResult) error {
	enc := json.NewEncoder(w)
	return enc.Encode(parallelJSONData{Pieces: buildParallelPieces(results), Colors: parallelColors(results)})
}

// parallelView はテンプレートへ渡す描画データ。
//
// このビューは他ビュー (ダッシュボード / 時間配分ビュー) と違い**自動更新しない** (0134)。
// 振り返り用のインタラクティブ分析ビュー (日別ページング・日の選択・全期間トグル・タスク別
// ドリルダウン) なので、定期的な全再描画は操作の邪魔になる。ロード時のスナップショットを表示し、
// 最新データが要るときはユーザーが手動リロードする。そのため interval は受け取らない。
type parallelView struct {
	PiecesJSON template.JS
	ColorsJSON template.JS
	HasData    bool
}

func renderParallel(w io.Writer, results []taskWorktimeResult) error {
	pieces := buildParallelPieces(results)
	colors := parallelColors(results)
	pb, err := json.Marshal(pieces)
	if err != nil {
		return err
	}
	cb, err := json.Marshal(colors)
	if err != nil {
		return err
	}
	return parallelTemplate.Execute(w, parallelView{
		PiecesJSON: template.JS(pb), //nolint:gosec // json.Marshal 済み (HTML エスケープ有効)
		ColorsJSON: template.JS(cb), //nolint:gosec // 同上
		HasData:    len(pieces) > 0,
	})
}

//go:embed webassets/parallel.html webassets/parallel.css webassets/parallel.js
var parallelAssets embed.FS

// parallelTemplate はページ骨格 (parallel.html) に CSS (parallel.css) と JS (parallel.js) を
// ロード時に流し込んで組み立てる。CSS/JS を実ファイルに切り出すことで、エディタ補完/lint/整形と
// JS 単体テスト (webassets/parallel.test.js) を効かせつつ、単一の自己完結ページ (依存最小・
// //go:embed で単一バイナリ) は維持する。JS は素のロジックのみで、データは HTML の bootstrap
// script が window.PIECES / window.COLORS へ注入する。
var parallelTemplate = template.Must(template.New("parallel").Parse(buildParallelHTML() + viewNavTmpl))

// buildParallelHTML は骨格 HTML のプレースホルダに CSS / JS を差し込んだテンプレート文字列を返す。
// 埋め込む JS/CSS はテンプレート action ({{...}}) を含まないので、後段の template.Parse でそのまま
// 素通しされる (データ注入は骨格側の bootstrap script が {{.PiecesJSON}} 等で行う)。
func buildParallelHTML() string {
	html := mustParallelAsset("webassets/parallel.html")
	html = strings.Replace(html, "/*__PARALLEL_CSS__*/", mustParallelAsset("webassets/parallel.css"), 1)
	html = strings.Replace(html, "/*__PARALLEL_JS__*/", mustParallelAsset("webassets/parallel.js"), 1)
	return html
}

func mustParallelAsset(name string) string {
	b, err := parallelAssets.ReadFile(name)
	if err != nil {
		panic("parallel asset missing: " + name + ": " + err.Error())
	}
	return string(b)
}
