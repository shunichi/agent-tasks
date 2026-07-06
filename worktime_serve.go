package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"slices"
	"strings"
	"time"
)

// worktime の可視化 (serve の /worktime ルート)。0104 で「時刻タイムライン」から
// **時間配分ビュー**へ作り直した。一次ビューは 日/週/月 × プロジェクトの積み上げバー
// (表示中で最も稼働の多い期間を 100%幅の基準にしてスケールを揃える)。プロジェクト帯をタップ
// すると、その期間だけのタスク別内訳 (時間量ランキング) へドリルダウンする。
//
// 稼働は短いバーストの集まりなので、旧実装の「24h 絶対時刻トラック」ではスマホ幅で帯が
// 針化して読めなかった (0104 の検討で判明)。時刻・並列度の可視化は PC 限定で別タスク (0127)。
//
// サーバは稼働区間を「日単位に分割・集計したエントリ」+「プロジェクト色」を JSON で埋め込む
// だけにし、日/週/月 の集計・スケール計算・ドリルダウンはクライアント JS が行う (トグルや
// タップを round-trip 無しで扱うため)。serve と同じく外部依存なしの自己完結 HTML。

// wtEntry は 1 日 × 1 タスクの稼働 (秒)。区間を日境界で分割して集計したもの。
// 日単位まで割っておけば、クライアントは日/週/月のどれにも一意に振り分けられる。
type wtEntry struct {
	Date    string `json:"d"`  // ローカル日付 "2006-01-02"
	Project string `json:"p"`  //
	ID      string `json:"id"` //
	Title   string `json:"ti"` //
	Seconds int64  `json:"s"`  //
}

// buildWorktimeEntries は横断集計を日境界で分割し、(日, project, id) ごとに秒を合算する。
// 出力は日の新しい順 → project 名 → id の決定的順序。
func buildWorktimeEntries(results []taskWorktimeResult) []wtEntry {
	type key struct{ date, proj, id string }
	agg := map[key]*wtEntry{}
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
				k := key{dayStart.Format("2006-01-02"), r.Project, r.ID}
				e := agg[k]
				if e == nil {
					e = &wtEntry{Date: k.date, Project: r.Project, ID: r.ID, Title: r.Title}
					agg[k] = e
				}
				e.Seconds += int64(pieceEnd.Sub(cur).Seconds())
				cur = pieceEnd
			}
		}
	}
	out := make([]wtEntry, 0, len(agg))
	for _, e := range agg {
		out = append(out, *e)
	}
	slices.SortFunc(out, func(a, b wtEntry) int {
		return cmp.Or(strings.Compare(b.Date, a.Date), strings.Compare(a.Project, b.Project), strings.Compare(a.ID, b.ID))
	})
	return out
}

// projectColors はプロジェクトに決定的な色を割り当てる。色はプロジェクト単位なので色数が少なく
// (旧実装のタスク単位 16 色の衝突が起きない)、名前ソート順で安定する (スコープが変わっても不変)。
// 判別しやすい 8 色相を巡回し、9 色目以降は明度を下げて衝突を避ける。
func projectColors(entries []wtEntry) map[string]string {
	seen := map[string]bool{}
	var names []string
	for _, e := range entries {
		if !seen[e.Project] {
			seen[e.Project] = true
			names = append(names, e.Project)
		}
	}
	slices.Sort(names)
	hues := []int{210, 30, 150, 340, 265, 185, 12, 55}
	out := make(map[string]string, len(names))
	for i, p := range names {
		hue := hues[i%len(hues)]
		light := 60 - 9*(i/len(hues))
		if light < 34 {
			light = 34
		}
		out[p] = fmt.Sprintf("hsl(%d,68%%,%d%%)", hue, light)
	}
	return out
}

// worktimeView はテンプレートへ渡す描画データ。
type worktimeView struct {
	EntriesJSON template.JS // wtEntry の配列 (JSON)
	ColorsJSON  template.JS // project -> css color (JSON)
	Interval    int
	Refresh     bool
	Now         string
	HasData     bool
}

func renderTimeline(w io.Writer, results []taskWorktimeResult, interval int, now time.Time) error {
	entries := buildWorktimeEntries(results)
	colors := projectColors(entries)
	eb, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	cb, err := json.Marshal(colors)
	if err != nil {
		return err
	}
	return worktimeTemplate.Execute(w, worktimeView{
		EntriesJSON: template.JS(eb), //nolint:gosec // json.Marshal 済み (HTML エスケープ有効)
		ColorsJSON:  template.JS(cb), //nolint:gosec // 同上
		Interval:    interval,
		Refresh:     interval > 0,
		Now:         now.Format("2006-01-02 15:04:05"),
		HasData:     len(entries) > 0,
	})
}

var worktimeTemplate = template.Must(template.New("worktime").Parse(worktimeHTML))

// worktimeHTML は自己完結ページ。Go の raw 文字列 (backtick) なので、埋め込む JS では
// テンプレートリテラル (backtick) を使わず文字列連結 (+) で組む (backtick 衝突を避けるため)。
const worktimeHTML = `<!doctype html>
<html lang="ja">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
{{if .Refresh}}<meta http-equiv="refresh" content="{{.Interval}}">{{end}}
<title>agent-tasks — 稼働時間</title>
<style>
  :root {
    --bg:#0f1115; --panel:#171a21; --card:#1d222b; --card2:#232936; --border:#2a2f3a;
    --fg:#e6e8ec; --dim:#8b93a1; --dim2:#636b78; --accent:#4a9eff; --id:#ffd479;
    --mono: ui-monospace, "SFMono-Regular", "Menlo", "Consolas", monospace;
    --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", "Hiragino Sans", "Noto Sans JP", sans-serif;
  }
  * { box-sizing:border-box; }
  body { margin:0; padding:0 0 2rem; background:var(--bg); color:var(--fg); font-family:var(--sans);
         font-size:15px; line-height:1.5; -webkit-font-smoothing:antialiased; }
  header { position:sticky; top:0; z-index:10; background:var(--panel);
           border-bottom:1px solid var(--border); padding:.7rem 1rem; }
  header h1 { margin:0; font-size:1.1rem; display:flex; align-items:center; gap:.6rem; }
  header h1 a { color:var(--accent); text-decoration:none; font-size:.85rem; font-weight:400; }
  header .meta { color:var(--dim); font-size:.78rem; margin-top:.15rem;
                 font-family:var(--mono); font-variant-numeric:tabular-nums; }
  main { padding:.75rem; max-width:900px; margin:0 auto; }

  /* 粒度トグル (日/週/月) */
  .gran { display:flex; gap:.3rem; background:var(--card); border:1px solid var(--border);
          border-radius:.5rem; padding:.2rem; margin:0 0 .9rem; max-width:280px; }
  .gran button { flex:1; background:transparent; border:none; color:var(--dim); font-family:var(--sans);
          font-size:.85rem; padding:.4rem 0; border-radius:.35rem; cursor:pointer; font-weight:600; }
  .gran button.on { background:var(--accent); color:#08111f; }

  /* プロジェクト凡例 */
  .plegend { display:flex; flex-wrap:wrap; gap:.35rem .8rem; margin:0 0 1rem;
             font-size:.74rem; font-family:var(--mono); }
  .plegend .it { display:inline-flex; align-items:center; gap:.34rem; }
  .plegend .sw { width:.72rem; height:.72rem; border-radius:2px; }
  .plegend .tm { color:var(--dim); }

  /* 期間行 */
  .prow { margin-bottom:.7rem; }
  .prow .hd { display:flex; justify-content:space-between; font-size:.75rem; font-family:var(--mono);
              color:var(--dim); margin-bottom:.24rem; }
  .prow .hd .d { color:var(--fg); font-weight:600; }
  /* rail = 「最も稼働の多い期間 = 100%幅」の基準。stack はその中を実量比で占める */
  .rail { position:relative; height:1.9rem; background:var(--card); border:1px solid var(--border);
          border-radius:6px; overflow:hidden; }
  .stack { position:absolute; left:0; top:0; bottom:0; display:flex; overflow:hidden; border-radius:5px; }
  .seg { position:relative; display:flex; align-items:center; padding:0 .4rem; min-width:0; cursor:pointer;
         font-size:.72rem; font-weight:700; color:rgba(0,0,0,.78); white-space:nowrap; overflow:hidden;
         transition:filter .12s; }
  .seg:hover { filter:brightness(1.12); }
  .seg small { font-weight:500; opacity:.85; margin-left:.3rem; }

  /* 詳細ビュー (プロジェクト → その期間のタスク) */
  .back { background:none; border:none; color:var(--accent); font-family:var(--sans); font-size:.85rem;
          cursor:pointer; padding:.1rem 0; margin:0 0 .7rem; }
  .dhead { display:flex; align-items:center; gap:.5rem; margin-bottom:.15rem; }
  .dhead .sw { width:.9rem; height:.9rem; border-radius:3px; }
  .dhead .nm { font-size:1.1rem; font-weight:700; }
  .dhead .tm { margin-left:auto; font-family:var(--mono); color:var(--dim); font-size:.85rem;
               font-variant-numeric:tabular-nums; }
  .dsub { color:var(--dim); font-size:.75rem; font-family:var(--mono); margin:0 0 .9rem; }
  .bars { display:flex; flex-direction:column; gap:.55rem; }
  .brow .bt { display:flex; align-items:baseline; gap:.45rem; font-size:.8rem; margin-bottom:.18rem; }
  .brow .bt .id { color:var(--id); font-family:var(--mono); font-weight:700; flex:none; }
  .brow .bt .ti { flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .brow .bt .dr { color:var(--dim); font-family:var(--mono); font-variant-numeric:tabular-nums; flex:none; }
  .btrack { height:.65rem; background:var(--card); border-radius:4px; overflow:hidden; }
  .bfill { height:100%; border-radius:4px; min-width:2px; }

  .hint { text-align:center; color:var(--dim2); font-size:.72rem; margin-top:1.2rem; }
  .empty { color:var(--dim); text-align:center; margin-top:3rem; }
</style>
</head>
<body>
<header>
  <h1>稼働時間 <a href="/">← 一覧</a></h1>
  <div class="meta" id="meta"></div>
</header>
<main id="app"></main>
{{if .HasData}}
<script>
(function(){
  var ENTRIES = {{.EntriesJSON}};
  var COLORS  = {{.ColorsJSON}};
  var app = document.getElementById("app");
  var metaEl = document.getElementById("meta");
  var LIMIT = {day:14, week:12, month:60};
  var WD = ["日","月","火","水","木","金","土"];

  var ESC = {"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;"};
  function esc(s){ return String(s).replace(/[&<>"]/g, function(c){ return ESC[c]; }); }
  function colorOf(p){ return COLORS[p] || "#888"; }
  function hm(sec){ // humanizeDuration (Go 側) に合わせる
    sec = Math.round(sec);
    if(sec < 60) return sec + "s";
    var m = Math.floor(sec/60);
    if(m < 60) return m + "m";
    var h = Math.floor(m/60), mm = m%60;
    return mm ? h + "h" + mm + "m" : h + "h";
  }
  function pad2(n){ return String(n).padStart(2, "0"); }
  function parseD(s){ var a=s.split("-"); return new Date(+a[0], +a[1]-1, +a[2]); }
  function pkey(dstr, gran){
    var dt = parseD(dstr);
    if(gran==="day")   return {k:dstr, label:(dt.getMonth()+1)+"/"+dt.getDate()+" ("+WD[dt.getDay()]+")"};
    if(gran==="month") return {k:dstr.slice(0,7), label:dt.getFullYear()+"/"+(dt.getMonth()+1)};
    var off=(dt.getDay()+6)%7, mon=new Date(dt); mon.setDate(dt.getDate()-off); // 月曜始まり
    var mk=mon.getFullYear()+"-"+pad2(mon.getMonth()+1)+"-"+pad2(mon.getDate());
    return {k:mk, label:(mon.getMonth()+1)+"/"+mon.getDate()+" の週"};
  }

  // ---- 状態 (粒度 + ドリルダウン先) を URL hash に保持し、meta refresh をまたいで復元 ----
  var state = {gran:"week", proj:null, period:null, plabel:null};
  function loadState(){
    var h = new URLSearchParams(location.hash.slice(1));
    if(h.get("g")) state.gran = h.get("g");
    state.proj = h.get("p"); state.period = h.get("pk"); state.plabel = h.get("pl");
  }
  function saveState(){
    var h = new URLSearchParams(); h.set("g", state.gran);
    if(state.proj){ h.set("p", state.proj); h.set("pk", state.period); h.set("pl", state.plabel); }
    history.replaceState(null, "", "#" + h.toString());
  }

  function render(){
    saveState();
    if(state.proj) renderDetail(); else renderOverview();
  }

  function renderOverview(){
    var g = state.gran, periods = {};
    ENTRIES.forEach(function(e){
      var pk = pkey(e.d, g);
      if(!periods[pk.k]) periods[pk.k] = {label:pk.label, byp:{}, tot:0};
      periods[pk.k].byp[e.p] = (periods[pk.k].byp[e.p]||0) + e.s;
      periods[pk.k].tot += e.s;
    });
    var keys = Object.keys(periods).sort().reverse().slice(0, LIMIT[g]);
    // スケール統一: 表示中で最も稼働の多い期間を 100%幅の基準にする (期間をまたいで量を比較できる)
    var maxTot = Math.max.apply(null, [1].concat(keys.map(function(k){ return periods[k].tot; })));
    // 凡例/合計 (表示中の期間のみ)
    var projTot = {}, grand = 0;
    keys.forEach(function(k){
      grand += periods[k].tot;
      for(var p in periods[k].byp) projTot[p] = (projTot[p]||0) + periods[k].byp[p];
    });
    var legOrder = Object.keys(projTot).sort(function(a,b){ return projTot[b]-projTot[a]; });
    var leg = legOrder.map(function(p){
      return '<span class="it"><span class="sw" style="background:'+colorOf(p)+'"></span>'+
             '<span>'+esc(p)+'</span><span class="tm">'+hm(projTot[p])+'</span></span>';
    }).join("");

    var rows = keys.map(function(k){
      var pd = periods[k];
      var ps = Object.keys(pd.byp).sort(function(a,b){ return pd.byp[b]-pd.byp[a]; });
      var segs = ps.map(function(p){
        var s = pd.byp[p], railPct = s/maxTot*100; // レール(=最大期間)に対する実量幅
        var lbl = railPct>18 ? esc(p)+'<small>'+hm(s)+'</small>' : railPct>9 ? esc(p) : "";
        return '<div class="seg" style="flex:'+s+' 0 0;background:'+colorOf(p)+'"'+
               ' data-proj="'+esc(p)+'" data-period="'+k+'" data-plabel="'+esc(pd.label)+'"'+
               ' title="'+esc(p)+' '+hm(s)+'">'+lbl+'</div>';
      }).join("");
      var barPct = pd.tot/maxTot*100; // 最大期間を 100%幅とした相対長
      return '<div class="prow"><div class="hd"><span class="d">'+esc(pd.label)+'</span>'+
             '<span>'+hm(pd.tot)+'</span></div>'+
             '<div class="rail"><div class="stack" style="width:'+barPct+'%">'+segs+'</div></div></div>';
    }).join("");

    var spanTxt = g==="day" ? "直近"+keys.length+"日" : g==="month" ? keys.length+"ヶ月" : "直近"+keys.length+"週";
    metaEl.textContent = spanTxt+" · 合計 "+hm(grand)+" · 最大 "+hm(maxTot)+"=全幅";
    app.innerHTML =
      '<div class="gran">'+
        '<button data-g="day" class="'+(g==="day"?"on":"")+'">日</button>'+
        '<button data-g="week" class="'+(g==="week"?"on":"")+'">週</button>'+
        '<button data-g="month" class="'+(g==="month"?"on":"")+'">月</button>'+
      '</div>'+
      '<div class="plegend">'+leg+'</div>'+
      rows+
      '<div class="hint">色帯をタップ → その期間のタスク内訳へ</div>';

    Array.prototype.forEach.call(app.querySelectorAll(".gran button"), function(b){
      b.onclick = function(){ state.gran = b.dataset.g; render(); };
    });
    Array.prototype.forEach.call(app.querySelectorAll(".seg"), function(s){
      s.onclick = function(){
        state.proj = s.dataset.proj; state.period = s.dataset.period; state.plabel = s.dataset.plabel;
        scrollTo(0, 0); render();
      };
    });
  }

  function renderDetail(){
    var p = state.proj, byTask = {}, tot = 0;
    ENTRIES.filter(function(e){ return e.p===p && pkey(e.d, state.gran).k===state.period; })
      .forEach(function(e){
        if(!byTask[e.id]) byTask[e.id] = {id:e.id, ti:e.ti, s:0};
        byTask[e.id].s += e.s; tot += e.s;
      });
    var tasks = Object.keys(byTask).map(function(k){ return byTask[k]; })
      .sort(function(a,b){ return b.s-a.s; });
    var max = Math.max.apply(null, [1].concat(tasks.map(function(t){ return t.s; })));
    var bars = tasks.map(function(t){
      return '<div class="brow"><div class="bt"><span class="id">#'+esc(t.id)+'</span>'+
             '<span class="ti">'+esc(t.ti)+'</span><span class="dr">'+hm(t.s)+'</span></div>'+
             '<div class="btrack"><div class="bfill" style="width:'+(t.s/max*100)+'%;background:'+colorOf(p)+'"></div></div></div>';
    }).join("");
    metaEl.textContent = state.plabel+" · "+p;
    app.innerHTML =
      '<button class="back">← '+esc(state.plabel)+' に戻る</button>'+
      '<div class="dhead"><span class="sw" style="background:'+colorOf(p)+'"></span>'+
      '<span class="nm">'+esc(p)+'</span><span class="tm">'+hm(tot)+'</span></div>'+
      '<div class="dsub">'+esc(state.plabel)+' · タスク '+tasks.length+' 件 · 多い順</div>'+
      '<div class="bars">'+bars+'</div>';
    app.querySelector(".back").onclick = function(){ state.proj = null; state.period = null; render(); };
  }

  // スクロール位置を meta refresh (ページ再読込) をまたいで復元する。
  try {
    var sy = sessionStorage.getItem("wt_scroll");
    addEventListener("scroll", function(){ sessionStorage.setItem("wt_scroll", String(scrollY)); }, {passive:true});
    loadState(); render();
    if(sy && !state.proj) scrollTo(0, Number(sy));
  } catch(_) { loadState(); render(); }
})();
</script>
{{else}}
<script>
  document.getElementById("app").innerHTML =
    '<p class="empty">稼働記録がまだありません (この機能の導入後に着手したタスクから記録されます)</p>';
</script>
{{end}}
</body>
</html>
`
