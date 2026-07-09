package main

import (
	"cmp"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// serve は `agent-tasks -w --all-projects` 相当の一覧を、同一 LAN のスマホから閲覧するための
// 簡易 HTTP サーバ。依存を増やさず net/http + html/template だけで実装する。
//
// 方針 (タスク 0058 の合意):
//   - HTML 直生成 (サーバ側レンダリング)。既存の selectTasks / session 状態 / blocked 経過を流用。
//   - 既定は 127.0.0.1 に bind (localhost だけ)。--addr で明示すると 0.0.0.0 等に公開できる
//     (LAN 内前提なので認証なし)。
//   - watch 相当の自動更新は <meta http-equiv="refresh"> (--interval 秒、既定 5。0 で無効)。
//   - スマホ向けのレスポンシブなカードレイアウト。SESSION 状態 (working/waiting/ended) と
//     blocked 経過も出す。各タスクの session: URL / prs: をリンクとして開ける。

const serveDefaultAddr = "127.0.0.1:8080"

func cmdServe(args []string) error {
	addr := serveDefaultAddr
	interval := 5
	var filterProjects []string
	var filterStatus, filterKind string
	showAll := false
	allProjects := true // ダッシュボードは既定で全 project 横断 (`-w --all-projects` 相当)

	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch a {
		case "--addr":
			v, err := s.value("--addr")
			if err != nil {
				return err
			}
			addr = v
		case "--interval":
			v, err := s.value("--interval")
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return usagef("--interval must be a non-negative integer (秒): %q", v)
			}
			interval = n
		case "--project":
			v, err := s.value("--project")
			if err != nil {
				return err
			}
			filterProjects = append(filterProjects, v)
			allProjects = false
		case "--projects":
			v, err := s.value("--projects")
			if err != nil {
				return err
			}
			filterProjects = append(filterProjects, splitProjects(v)...)
			allProjects = false
		case "--all-projects":
			allProjects = true
		case "--status":
			v, err := s.value("--status")
			if err != nil {
				return err
			}
			filterStatus = v
		case "--kind":
			v, err := s.value("--kind")
			if err != nil {
				return err
			}
			if v != kindHuman && v != kindCode {
				return usagef("--kind must be %s|%s (got %q)", kindHuman, kindCode, v)
			}
			filterKind = v
		case "--all", "-a":
			showAll = true
		default:
			return usagef("unknown option: %s", a)
		}
	}
	if pos := s.rest(); len(pos) > 0 {
		return usagef("unexpected argument: %s", pos[0])
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		rows, _, _, err := selectTasks(filterStatus, filterProjects, showAll, allProjects, false, "", false, filterKind)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := renderDashboard(w, rows, interval, time.Now()); err != nil {
			// ヘッダは送出済みなので描画途中のエラーは stderr に出すだけ (接続はそのまま切れる)。
			fmt.Fprintf(os.Stderr, "serve: render error: %v\n", err)
		}
	})
	// /worktime: 稼働区間のタイムライン可視化 (0103)。スコープは / と同じ。
	mux.HandleFunc("/worktime", func(w http.ResponseWriter, r *http.Request) {
		rows, _, _, err := selectTasks("", filterProjects, true, allProjects, false, "", false, filterKind)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		now := time.Now()
		results, err := collectWorktimes(rows, now)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOut := r.URL.Query().Get("format") == "json"
		// ?view=parallel: 時間帯・並列ビュー (0127。PC 限定)。既存の時間配分ビューとは別描画。
		if r.URL.Query().Get("view") == "parallel" {
			if jsonOut {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				if err := renderParallelJSON(w, results); err != nil {
					fmt.Fprintf(os.Stderr, "serve: parallel json error: %v\n", err)
				}
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			// parallel ビューは自動更新しない (0134) ので interval は渡さない。
			if err := renderParallel(w, results); err != nil {
				fmt.Fprintf(os.Stderr, "serve: parallel render error: %v\n", err)
			}
			return
		}
		// ?format=json: データだけ返す (ページの自動更新ポーリング用。全ページ再読込しない)。
		if jsonOut {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if err := renderTimelineJSON(w, results); err != nil {
				fmt.Fprintf(os.Stderr, "serve: timeline json error: %v\n", err)
			}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := renderTimeline(w, results, interval, now); err != nil {
			fmt.Fprintf(os.Stderr, "serve: timeline render error: %v\n", err)
		}
	})

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	printServeURLs(addr, ln.Addr(), interval)
	return http.Serve(ln, mux)
}

// printServeURLs は起動時に開くべき URL を案内する。localhost bind なら localhost だけ、
// 全インターフェース (host 空 / 0.0.0.0 / ::) に公開したときは LAN の IP も列挙して
// スマホから開くアドレスを分かるようにする。
func printServeURLs(addr string, ln net.Addr, interval int) {
	port := serveListenPort(ln, addr)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	exposed := host == "" || host == "0.0.0.0" || host == "::" || host == "[::]"

	// どの commit 時点のバイナリで動いているか起動時に分かるようにする (リビルド忘れの検知にも役立つ)。
	fmt.Fprintln(os.Stderr, formatVersion(readVCSInfo()))
	fmt.Fprintf(os.Stderr, "agent-tasks serve: %s で待受中", addr)
	if interval > 0 {
		fmt.Fprintf(os.Stderr, " (自動更新 %d 秒)", interval)
	}
	fmt.Fprintln(os.Stderr)
	if exposed {
		ips := lanIPv4s()
		if len(ips) == 0 {
			fmt.Fprintf(os.Stderr, "  LAN: http://<このマシンの IP>:%s/\n", port)
		}
		for i, ip := range ips {
			// 先頭 (最も家庭内 LAN らしい IP) にだけ矢印を付ける。docker/仮想ブリッジ (172.x) が
			// 多数あっても、スマホから開くべき候補が一目で分かるようにする。
			hint := ""
			if i == 0 {
				hint = "  ← スマホからこれを開く"
			}
			fmt.Fprintf(os.Stderr, "  LAN: http://%s:%s/%s\n", ip, port, hint)
		}
		fmt.Fprintf(os.Stderr, "  ローカル: http://127.0.0.1:%s/\n", port)
	} else {
		fmt.Fprintf(os.Stderr, "  ローカル: http://%s:%s/\n", host, port)
		fmt.Fprintln(os.Stderr, "  (LAN のスマホから見るには --addr :8080 のように公開先を指定してください)")
	}
	fmt.Fprintln(os.Stderr, "  停止: Ctrl-C")
}

// serveListenPort は実際に待ち受けているポート番号を返す (addr が ":0" 等でも実ポートが分かる)。
func serveListenPort(ln net.Addr, addr string) string {
	if tcp, ok := ln.(*net.TCPAddr); ok {
		return strconv.Itoa(tcp.Port)
	}
	if _, p, err := net.SplitHostPort(addr); err == nil {
		return p
	}
	return addr
}

// lanIPv4s は非ループバックの IPv4 アドレスを列挙する (スマホから開く URL の案内用)。
// 家庭内 LAN でよく使う 192.168/16・10/8 を先頭に、docker/仮想ブリッジで多用される 172.16/12 を
// 後ろに並べ替える (スマホから開くべき候補を先頭に出すため。全 IP は列挙したまま)。
func lanIPv4s() []string {
	var out []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil {
			continue // IPv6 は案内では省く (LAN のスマホ用途では IPv4 で十分)
		}
		out = append(out, ip4.String())
	}
	slices.SortStableFunc(out, func(a, b string) int { return cmp.Compare(lanIPRank(a), lanIPRank(b)) })
	return out
}

// lanIPRank は家庭内 LAN らしさの順位 (小さいほど先頭)。192.168 > 10 > その他 > 172.16-31 (docker 常用)。
func lanIPRank(ip string) int {
	switch {
	case strings.HasPrefix(ip, "192.168."):
		return 0
	case strings.HasPrefix(ip, "10."):
		return 1
	case strings.HasPrefix(ip, "172."):
		return 3
	default:
		return 2
	}
}

// --- ビュー生成 -------------------------------------------------------------

type dashRow struct {
	Project       string
	ID            string
	Status        string
	StatusClass   string
	Title         string
	Updated       string
	SessionState  string // in-progress のみ (working/waiting/ended/unknown)
	BlockedFor    string // blocked のみ (経過)
	BlockedReason string
	SessionURL    string       // session: が http URL のとき (web/ブラウザで開く用のフォールバック)
	SessionAppURL template.URL // claude://code/... (Claude アプリを直接開くディープリンク。無いとき空)
	PRs           []string     // prs: の URL 群
}

// dashProjGroup は状態セクション内の project 別サブグループ。
type dashProjGroup struct {
	Project string
	Rows    []dashRow
}

type dashGroup struct {
	Key      string // セクションキー (waiting/review/working/other。CSS クラスにも使う)
	Label    string // 見出しラベル (日本語)
	Count    int    // セクション内の総タスク数 (見出しの件数表示用)
	Projects []dashProjGroup
}

type dashData struct {
	Groups   []dashGroup
	Count    int
	Now      string
	Interval int
	Refresh  bool
}

// dashSections は状態別セクションの固定表示順とラベル。「今すぐ対応が要るもの」から並べる:
// 入力待ち → レビュー待ち → 実行中 → その他。空セクションは描画時に飛ばす。
var dashSections = []struct{ Key, Label string }{
	{"waiting", "入力待ち"},
	{"review", "レビュー待ち"},
	{"working", "実行中"},
	{"other", "その他"},
}

// statusClass は status を CSS クラス名に使える形へ (in-progress はそのままハイフンで可)。
func statusClass(status string) string {
	if status == "" {
		return "unknown"
	}
	return status
}

// taskSection は行が入る状態セクションのキーを返す (1 行は 1 セクションのみ)。
// in-progress かつ SESSION マーカーが waiting/working のものだけ waiting/working に入れ、
// status=review は review、それ以外 (todo/blocked/done や unknown/ended の in-progress) は other。
func taskSection(status, sessionState string) string {
	switch sessionState {
	case sessWaiting:
		return "waiting"
	case sessWorking:
		return "working"
	}
	if status == "review" {
		return "review"
	}
	return "other"
}

// isHTTPURL は文字列が http(s) URL としてリンクしてよい形かをゆるく判定する。
func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// claudeAppURL は claude.ai の Claude Code セッション URL を、Claude アプリを直接開く
// カスタムスキーム URL (claude://code/<session-id>) に変換する。
// 例: https://claude.ai/code/session_01ABC → claude://code/session_01ABC。
// claude.ai/code/ 配下でない URL は変換対象外 (空を返す = アプリリンク無し)。
// universal link (https) はアプリ未インストール等でブラウザに落ちるため、確実にアプリを
// 開きたいときの導線としてこのスキーム URL を併記する (session-id は英数字+アンダースコアのみで
// URL エンコード不要)。
func claudeAppURL(sessionURL string) string {
	for _, p := range []string{"https://claude.ai/code/", "http://claude.ai/code/"} {
		if rest, ok := strings.CutPrefix(sessionURL, p); ok && rest != "" {
			return "claude://code/" + rest
		}
	}
	return ""
}

func buildDashData(rows []Task, interval int, now time.Time) dashData {
	d := dashData{
		Count:    len(rows),
		Now:      now.Format("2006-01-02 15:04:05"),
		Interval: interval,
		Refresh:  interval > 0,
	}
	// rows は project → id 順にソート済み。状態セクションごとに振り分け、入力順を保つ
	// (各セクション内は project → id 順になる)。
	bySection := map[string][]dashRow{}
	for _, t := range rows {
		r := dashRow{
			Project:       t.Project,
			ID:            t.ID,
			Status:        t.Status,
			StatusClass:   statusClass(t.Status),
			Title:         displayTitle(t),
			Updated:       displayDateOr(t.Updated, t.Created),
			BlockedReason: t.BlockedReason,
			PRs:           t.PRs,
		}
		if t.Status == "in-progress" && !t.IsHuman() {
			if st, ok := taskSessionState(t); ok {
				r.SessionState = st.State
			} else {
				r.SessionState = "unknown"
			}
		}
		if t.Status == "blocked" && t.BlockedAt != "" {
			r.BlockedFor = humanizeSince(t.BlockedAt, now)
		}
		if isHTTPURL(t.Session) {
			r.SessionURL = t.Session
			r.SessionAppURL = template.URL(claudeAppURL(t.Session)) //nolint:gosec // claude.ai/code/ 前提で自前生成
		}
		key := taskSection(t.Status, r.SessionState)
		bySection[key] = append(bySection[key], r)
	}
	// 固定順にセクションを出す。空セクションは飛ばす。各セクション内は project 別に
	// サブグループ化する (rows は project→id 順なので、project が変わる境目で束ねればよい)。
	for _, sec := range dashSections {
		rows := bySection[sec.Key]
		if len(rows) == 0 {
			continue
		}
		g := dashGroup{Key: sec.Key, Label: sec.Label, Count: len(rows)}
		var cur *dashProjGroup
		for _, r := range rows {
			if cur == nil || cur.Project != r.Project {
				g.Projects = append(g.Projects, dashProjGroup{Project: r.Project})
				cur = &g.Projects[len(g.Projects)-1]
			}
			cur.Rows = append(cur.Rows, r)
		}
		d.Groups = append(d.Groups, g)
	}
	return d
}

func renderDashboard(w io.Writer, rows []Task, interval int, now time.Time) error {
	return dashTemplate.Execute(w, buildDashData(rows, interval, now))
}

var dashTemplate = template.Must(template.New("dashboard").Parse(dashHTML + viewNavTmpl))

const dashHTML = `<!doctype html>
<html lang="ja">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
{{if .Refresh}}<meta http-equiv="refresh" content="{{.Interval}}">{{end}}
<title>agent-tasks</title>
<style>
  :root {
    --bg: #0f1115; --panel: #171a21; --card: #1d222b; --border: #2a2f3a;
    --fg: #e6e8ec; --dim: #8b93a1; --accent: #4a9eff; --proj: #4dd6c1;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 0 0 2rem;
    background: var(--bg); color: var(--fg);
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Hiragino Sans", "Noto Sans JP", sans-serif;
    font-size: 15px; line-height: 1.5;
  }
  header {
    position: sticky; top: 0; z-index: 10;
    background: var(--panel); border-bottom: 1px solid var(--border);
    padding: 0.7rem 1rem;
  }
  header h1 { margin: 0; font-size: 1.1rem; display: flex; align-items: center; gap: 0.6rem; }
  header h1 a { color: var(--accent); text-decoration: none; font-size: 0.85rem; font-weight: 400; }
  header .meta { color: var(--dim); font-size: 0.8rem; margin-top: 0.15rem; }
  section { padding: 0 0.75rem; }
  section h2 {
    font-size: 0.9rem; letter-spacing: 0.02em;
    color: var(--fg); margin: 1.2rem 0.25rem 0.5rem; font-weight: 700;
    display: flex; align-items: center; gap: 0.4rem;
  }
  section h2::before { content: ""; width: 0.55rem; height: 0.55rem; border-radius: 50%; background: var(--dim); }
  section.sec-waiting h2::before { background: #f0883e; }
  section.sec-review  h2::before { background: #a371f7; }
  section.sec-working h2::before { background: #3fb950; }
  section.sec-other   h2::before { background: #6b7280; }
  section h2 .cnt { color: var(--dim); font-weight: 400; }
  .proj-group { margin: 0 0 0.6rem; }
  h3.proj {
    font-size: 0.85rem; color: var(--proj); font-weight: 700; letter-spacing: 0.02em;
    margin: 0.5rem 0.25rem 0.35rem; display: flex; align-items: center; gap: 0.35rem;
  }
  h3.proj::before { content: "▸"; color: var(--proj); }
  h3.proj .cnt { color: var(--dim); font-weight: 400; }
  .card {
    background: var(--card); border: 1px solid var(--border);
    border-left: 3px solid var(--border);
    border-radius: 8px; padding: 0.6rem 0.75rem; margin-bottom: 0.5rem;
  }
  .card.s-todo        { border-left-color: #6b7280; }
  .card.s-in-progress { border-left-color: #4a9eff; }
  .card.s-blocked     { border-left-color: #f0883e; }
  .card.s-review      { border-left-color: #a371f7; }
  .card.s-done        { border-left-color: #3fb950; }
  .row1 { display: flex; align-items: center; gap: 0.4rem; flex-wrap: wrap; margin-bottom: 0.3rem; }
  .id {
    color: #ffd479; font-weight: 700; font-variant-numeric: tabular-nums;
    font-size: 0.9rem; letter-spacing: 0.02em;
  }
  .badge {
    font-size: 0.72rem; padding: 0.05rem 0.4rem; border-radius: 999px;
    border: 1px solid var(--border); white-space: nowrap;
  }
  .badge.st-todo        { color: #9ca3af; }
  .badge.st-in-progress { color: #4a9eff; border-color: #234; }
  .badge.st-blocked     { color: #f0883e; }
  .badge.st-review      { color: #a371f7; }
  .badge.st-done        { color: #3fb950; }
  .badge.sess-working { color: #3fb950; border-color: #244a2f; }
  .badge.sess-waiting { color: #f0883e; border-color: #4a3520; background: #2a1e12; }
  .badge.sess-ended   { color: #8b93a1; }
  .badge.sess-unknown { color: #8b93a1; }
  .badge.blk { color: #f0883e; }
  .title { font-weight: 500; word-break: break-word; }
  .reason { color: #f0a868; font-size: 0.82rem; margin-top: 0.2rem; }
  .links { margin-top: 0.35rem; display: flex; gap: 0.4rem; flex-wrap: wrap; }
  .links a {
    font-size: 0.78rem; color: var(--accent); text-decoration: none;
    border: 1px solid var(--border); border-radius: 6px; padding: 0.1rem 0.5rem;
  }
  .links a:active { background: #23303f; }
  .links a.app { color: #0b0d10; background: var(--accent); border-color: var(--accent); font-weight: 600; }
  .links a.app:active { background: #3a86e0; }
  .upd { color: var(--dim); font-size: 0.75rem; margin-top: 0.35rem; }
  .empty { color: var(--dim); text-align: center; margin-top: 3rem; }
</style>
</head>
<body>
<header>
  <h1>agent-tasks</h1>
  <div class="meta">{{.Count}} tasks · {{.Now}}{{if .Refresh}} · 自動更新 {{.Interval}}s{{end}}</div>
  {{template "viewnav" "list"}}
</header>
{{range .Groups}}
<section class="sec-{{.Key}}">
  <h2>{{.Label}} <span class="cnt">({{.Count}})</span></h2>
  {{range .Projects}}
  <div class="proj-group">
    <h3 class="proj">{{.Project}} <span class="cnt">({{len .Rows}})</span></h3>
    {{range .Rows}}
    <article class="card s-{{.StatusClass}}">
      <div class="row1">
        <span class="id">#{{.ID}}</span>
        <span class="badge st-{{.StatusClass}}">{{.Status}}</span>
        {{if .SessionState}}<span class="badge sess-{{.SessionState}}">{{.SessionState}}</span>{{end}}
        {{if .BlockedFor}}<span class="badge blk">⏸ {{.BlockedFor}}</span>{{end}}
      </div>
      <div class="title">{{.Title}}</div>
      {{if .BlockedReason}}<div class="reason">{{.BlockedReason}}</div>{{end}}
      {{if or .SessionURL .PRs}}
      <div class="links">
        {{if .SessionAppURL}}<a class="app" href="{{.SessionAppURL}}">アプリで開く</a>{{end}}
        {{if .SessionURL}}<a href="{{.SessionURL}}" target="_blank" rel="noopener">web</a>{{end}}
        {{range .PRs}}<a href="{{.}}" target="_blank" rel="noopener">PR</a>{{end}}
      </div>
      {{end}}
      <div class="upd">updated {{.Updated}}</div>
    </article>
    {{end}}
  </div>
  {{end}}
</section>
{{end}}
{{if not .Groups}}<p class="empty">該当タスクなし</p>{{end}}
</body>
</html>
`
