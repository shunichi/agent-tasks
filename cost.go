package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// cost — タスクごとの Claude トークン消費 / 概算コストを、Claude Code の**ローカル
// transcript (JSONL)** から集計する ([[0101]])。既存の session-link (session_id) で
// タスク⇄セッションを解決し、そのセッションの transcript の usage を合算する。
//
// 方針 (ざっくり):
//   - タスクが使った全セッション (linkSessionIDs) の transcript を union で集計。
//   - 同一 message.id の重複行は 1 度だけ数える (Claude Code の JSONL は同じ応答を複数行に
//     書くことがあり、素朴に足すと二重計上になる)。
//   - started_at..completed_at (無ければ now まで) の時間窓で行を絞る。1 セッションで複数
//     タスクを回す batch でもタスク境界で切り分けられる (worktime と同じ窓)。
//   - トークン内訳 (入力/出力/キャッシュ読取/キャッシュ書込) と、モデル別価格での概算コストを出す。
//   - サブスク (Pro/Max) 利用時は「API 換算の目安」であって実請求額ではない。
//   - --record で frontmatter cost: に 1 行サマリを保存 (transcript は揮発データなので残す)。

// modelPrice は 100 万トークンあたりの単価 (USD)。入力・出力のみ持ち、キャッシュ系は
// 入力単価に固定倍率を掛けて出す (Anthropic の料金体系)。
type modelPrice struct{ in, out float64 }

// キャッシュ単価の倍率 (入力単価に対する): 書込 5m=1.25x / 1h=2x、読取=0.1x。
const (
	cacheWrite5mMul = 1.25
	cacheWrite1hMul = 2.0
	cacheReadMul    = 0.1
)

// modelPrices は主要モデルの単価表 (per MTok, USD)。日付サフィックス付き ID は正規化して引く。
// 価格は claude-api skill / 公式の値。改定時はここを更新する。
var modelPrices = map[string]modelPrice{
	"claude-opus-4-8":   {5, 25},
	"claude-opus-4-7":   {5, 25},
	"claude-opus-4-6":   {5, 25},
	"claude-opus-4-5":   {5, 25},
	"claude-sonnet-5":   {3, 15},
	"claude-sonnet-4-6": {3, 15},
	"claude-sonnet-4-5": {3, 15},
	"claude-haiku-4-5":  {1, 5},
}

// normalizeModelID は末尾の日付スナップショット (-YYYYMMDD) を落として単価表と突合する。
// 例: claude-haiku-4-5-20251001 → claude-haiku-4-5。
func normalizeModelID(model string) string {
	if i := strings.LastIndexByte(model, '-'); i > 0 {
		if suf := model[i+1:]; len(suf) == 8 {
			allDigit := true
			for _, r := range suf {
				if r < '0' || r > '9' {
					allDigit = false
					break
				}
			}
			if allDigit {
				return model[:i]
			}
		}
	}
	return model
}

// usageAgg はモデル単位のトークン集計。
type usageAgg struct {
	input, output, cacheRead, cacheWrite5m, cacheWrite1h int64
}

func (u usageAgg) total() int64 {
	return u.input + u.output + u.cacheRead + u.cacheWrite5m + u.cacheWrite1h
}

// costResult は 1 タスクのコスト集計結果 (表示 / JSON / 記録の共通データ)。
type costResult struct {
	Project, ID, Title string
	SessionIDs         []string // 集計対象セッション
	Transcripts        int      // 見つかった transcript ファイル数
	Models             []string // 出現したモデル (正規化後、昇順)
	UnpricedModels     []string // 単価表に無かったモデル
	Usage              usageAgg // 全モデル合算のトークン内訳
	USD                float64  // 概算コスト (unpriced 分は 0 加算)
	Priced             bool     // 全モデルに単価があった (概算が完全)
	MeasuredAt         time.Time
}

// transcriptLine は Claude Code の transcript JSONL 1 行 (assistant 行の usage 部分だけ拾う)。
type transcriptLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input       int64 `json:"input_tokens"`
			Output      int64 `json:"output_tokens"`
			CacheRead   int64 `json:"cache_read_input_tokens"`
			CacheCreate int64 `json:"cache_creation_input_tokens"`
			// 書込は 5m/1h で単価が違うので、分割があればそれを使う。
			CacheCreation *struct {
				E5m int64 `json:"ephemeral_5m_input_tokens"`
				E1h int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
}

// claudeProjectsDir は Claude Code の設定ディレクトリ配下の projects/ を返す
// (CLAUDE_CONFIG_DIR、未設定なら ~/.claude)。
func claudeProjectsDir() string {
	base := os.Getenv("CLAUDE_CONFIG_DIR")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, ".claude")
		}
	}
	return filepath.Join(base, "projects")
}

// findTranscripts は session_id の transcript (<projects>/*/<sid>.jsonl) を全 project dir 横断で探す。
// cwd 符号化ディレクトリ名は分からなくても session_id 一意なので、glob で拾う。
func findTranscripts(sid string) []string {
	if sid == "" {
		return nil
	}
	matches, _ := filepath.Glob(filepath.Join(claudeProjectsDir(), "*", sid+".jsonl"))
	return matches
}

// accumulateTranscript は 1 transcript を読み、usage を model 別に足す。win で時間窓クリップ
// (winStart 以前 / winEnd 以降の行は除外。ゼロ値は無制限)。seen で message.id の重複を除く。
func accumulateTranscript(path string, winStart, winEnd time.Time, seen map[string]bool, byModel map[string]*usageAgg) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024) // 長い行 (大きな tool 出力) に耐える
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var tl transcriptLine
		if err := json.Unmarshal(line, &tl); err != nil {
			continue // 壊れた行は飛ばす (集計は best-effort)
		}
		if tl.Message.Usage == nil || tl.Message.ID == "" {
			continue
		}
		if seen[tl.Message.ID] {
			continue // 重複行 (同一応答が複数回書かれる) は 1 度だけ
		}
		// 時間窓クリップ。timestamp が無い/壊れている行は窓指定があれば除外しない (含める)。
		if tm, ok := parseTranscriptTime(tl.Timestamp); ok {
			if !winStart.IsZero() && tm.Before(winStart) {
				continue
			}
			if !winEnd.IsZero() && tm.After(winEnd) {
				continue
			}
		}
		seen[tl.Message.ID] = true
		model := normalizeModelID(tl.Message.Model)
		agg := byModel[model]
		if agg == nil {
			agg = &usageAgg{}
			byModel[model] = agg
		}
		u := tl.Message.Usage
		agg.input += u.Input
		agg.output += u.Output
		agg.cacheRead += u.CacheRead
		if u.CacheCreation != nil && (u.CacheCreation.E5m != 0 || u.CacheCreation.E1h != 0) {
			agg.cacheWrite5m += u.CacheCreation.E5m
			agg.cacheWrite1h += u.CacheCreation.E1h
		} else {
			// 分割が無ければ全て 5m 扱い (Claude Code の既定 TTL)。
			agg.cacheWrite5m += u.CacheCreate
		}
	}
	return sc.Err()
}

// parseTranscriptTime は transcript の timestamp (RFC3339, 例 2026-07-01T21:56:52.664Z) をパースする。
func parseTranscriptTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if tm, err := time.Parse(time.RFC3339, s); err == nil {
		return tm, true
	}
	return time.Time{}, false
}

// computeTaskCost はタスクのコストを集計する。ok=false は session-link が無くセッションを
// 特定できないとき (hook 未導入 / start が session-link を書いていない)。
func computeTaskCost(t Task, now time.Time) (costResult, bool, error) {
	res := costResult{Project: t.Project, ID: t.ID, Title: t.Title, MeasuredAt: now, Priced: true}
	key := taskSessionKey(t)
	if key == "" {
		return res, false, nil
	}
	link, has := readSessionLink(key)
	if !has {
		return res, false, nil
	}
	res.SessionIDs = linkSessionIDs(link)

	// 時間窓 [started_at, completed_at|now] (worktime と同じ)。
	winStart, _ := parseTaskTime(t.StartedAt)
	winEnd := now
	if ct, ok := parseTaskTime(t.CompletedAt); ok {
		winEnd = ct
	}

	seen := map[string]bool{}
	byModel := map[string]*usageAgg{}
	for _, sid := range res.SessionIDs {
		for _, path := range findTranscripts(sid) {
			res.Transcripts++
			if err := accumulateTranscript(path, winStart, winEnd, seen, byModel); err != nil {
				return res, true, err
			}
		}
	}

	for model, agg := range byModel {
		res.Models = append(res.Models, model)
		res.Usage.input += agg.input
		res.Usage.output += agg.output
		res.Usage.cacheRead += agg.cacheRead
		res.Usage.cacheWrite5m += agg.cacheWrite5m
		res.Usage.cacheWrite1h += agg.cacheWrite1h
		if p, ok := modelPrices[model]; ok {
			res.USD += float64(agg.input)/1e6*p.in +
				float64(agg.output)/1e6*p.out +
				float64(agg.cacheRead)/1e6*p.in*cacheReadMul +
				float64(agg.cacheWrite5m)/1e6*p.in*cacheWrite5mMul +
				float64(agg.cacheWrite1h)/1e6*p.in*cacheWrite1hMul
		} else {
			res.Priced = false
			res.UnpricedModels = append(res.UnpricedModels, model)
		}
	}
	slices.Sort(res.Models)
	slices.Sort(res.UnpricedModels)
	return res, true, nil
}

// cmdCost は `agent-tasks cost [<project>] <id> [--json] [--record]`。
func cmdCost(args []string) error {
	jsonOut := false
	record := false
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch a {
		case "--json":
			jsonOut = true
		case "--record":
			record = true
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
	res, ok, err := computeTaskCost(t, now)
	if err != nil {
		return err
	}

	if jsonOut {
		return writeCostJSON(os.Stdout, res, ok)
	}
	if !ok {
		fmt.Printf("%s/%s %s: セッションを特定できません (session-link 未記録 / hook 未導入)\n", project, normalizeID(id), t.Title)
		fmt.Println("start で session-link されたタスクなら集計できます。")
		return nil
	}
	if res.Transcripts == 0 {
		fmt.Printf("%s/%s %s: transcript が見つかりません (セッション: %s)\n", project, normalizeID(id), t.Title, strings.Join(res.SessionIDs, ", "))
		fmt.Printf("Claude Code のログ (%s/*/<session>.jsonl) が消えている / 別マシンの可能性があります。\n", claudeProjectsDir())
		return nil
	}

	renderCost(os.Stdout, res, t)

	if record {
		summary := costSummaryLine(res)
		fields := []fmField{{"cost", summary}, {"updated", now.Format(time.RFC3339)}}
		if err := setFrontmatterFields(path, fields); err != nil {
			return fmt.Errorf("cost: の記録に失敗しました: %w", err)
		}
		fmt.Printf("\nrecorded cost: %s\n", summary)
		fmt.Printf("from: %s\n", storeRel(path)) // scoped sync 用
	}
	return nil
}

// costJSON は cost --json の機械可読表現。
type costJSON struct {
	Project            string   `json:"project"`
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Resolved           bool     `json:"resolved"` // session-link からセッションを特定できたか
	SessionIDs         []string `json:"session_ids,omitempty"`
	Transcripts        int      `json:"transcripts"`
	Models             []string `json:"models,omitempty"`
	UnpricedModels     []string `json:"unpriced_models,omitempty"`
	InputTokens        int64    `json:"input_tokens"`
	OutputTokens       int64    `json:"output_tokens"`
	CacheReadTokens    int64    `json:"cache_read_tokens"`
	CacheWrite5mTokens int64    `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens int64    `json:"cache_write_1h_tokens"`
	TotalTokens        int64    `json:"total_tokens"`
	USD                float64  `json:"usd"`
	CostComplete       bool     `json:"cost_complete"` // 全モデルに単価があり概算が完全か
	MeasuredAt         string   `json:"measured_at"`
}

func writeCostJSON(w *os.File, res costResult, resolved bool) error {
	u := res.Usage
	j := costJSON{
		Project: res.Project, ID: res.ID, Title: res.Title,
		Resolved: resolved, SessionIDs: res.SessionIDs, Transcripts: res.Transcripts,
		Models: res.Models, UnpricedModels: res.UnpricedModels,
		InputTokens: u.input, OutputTokens: u.output, CacheReadTokens: u.cacheRead,
		CacheWrite5mTokens: u.cacheWrite5m, CacheWrite1hTokens: u.cacheWrite1h,
		TotalTokens: u.total(), USD: res.USD, CostComplete: res.Priced,
		MeasuredAt: res.MeasuredAt.Format(time.RFC3339),
	}
	return newJSONEncoder(w).Encode(j)
}

// costSummary は show の末尾に出す、記録済み cost: の 1 行を返す。無ければ ""。
func costSummary(t Task, c colors) string {
	if t.Cost == "" {
		return ""
	}
	return c.bold + "cost:" + c.reset + " " + t.Cost
}

// costSummaryLine は frontmatter cost: に記録する 1 行サマリ。人間可読な目安。
func costSummaryLine(res costResult) string {
	u := res.Usage
	models := strings.Join(shortModels(res.Models), ",")
	if models == "" {
		models = "-"
	}
	return fmt.Sprintf("%s (%s tok: in %s/out %s/cache-r %s/cache-w %s; %s; %s)",
		usdStr(res.USD, res.Priced),
		humanCount(u.total()),
		humanCount(u.input), humanCount(u.output), humanCount(u.cacheRead),
		humanCount(u.cacheWrite5m+u.cacheWrite1h),
		models, res.MeasuredAt.Format("2006-01-02"))
}

// renderCost は人間向けのコスト内訳を描画する。
func renderCost(w *os.File, res costResult, t Task) {
	c := newColors()
	u := res.Usage
	fmt.Fprintf(w, "%s%s/%s%s %s\n", c.dim, res.Project, res.ID, c.reset, t.Title)
	fmt.Fprintf(w, "  セッション   : %d 個  transcript: %d ファイル\n", len(res.SessionIDs), res.Transcripts)
	fmt.Fprintf(w, "  入力         : %12s\n", humanInt(u.input))
	fmt.Fprintf(w, "  出力         : %12s\n", humanInt(u.output))
	fmt.Fprintf(w, "  キャッシュ読取: %12s\n", humanInt(u.cacheRead))
	fmt.Fprintf(w, "  キャッシュ書込: %12s  (5m %s / 1h %s)\n", humanInt(u.cacheWrite5m+u.cacheWrite1h), humanInt(u.cacheWrite5m), humanInt(u.cacheWrite1h))
	fmt.Fprintf(w, "  合計トークン : %12s\n", humanInt(u.total()))
	models := strings.Join(res.Models, ", ")
	if models == "" {
		models = "-"
	}
	fmt.Fprintf(w, "  モデル       : %s\n", models)
	if res.Priced {
		fmt.Fprintf(w, "  概算コスト   : %s%s%s  (API 換算の目安。サブスク利用なら実請求とは別)\n", c.bold, usdStr(res.USD, true), c.reset)
	} else {
		fmt.Fprintf(w, "  概算コスト   : %s%s%s  (未収載モデルあり: %s。その分はコスト未計上)\n", c.bold, usdStr(res.USD, false), c.reset, strings.Join(res.UnpricedModels, ", "))
	}
}

// usdStr は概算コストを "≈$0.42" 形式にする。priced=false は "≳$..." (下限扱い)。
func usdStr(usd float64, priced bool) string {
	prefix := "≈$"
	if !priced {
		prefix = "≳$"
	}
	return fmt.Sprintf("%s%.2f", prefix, usd)
}

// shortModels は claude-opus-4-8 → opus-4-8 のように prefix を落とした短縮名にする (サマリ用)。
func shortModels(models []string) []string {
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, strings.TrimPrefix(m, "claude-"))
	}
	return out
}

// humanCount は 1234 → "1.2k"、1234567 → "1.2M" と概数化する (サマリ用の短縮)。
func humanCount(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
}

// humanInt は桁区切り (12,340) を付ける (内訳表示用)。
func humanInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
