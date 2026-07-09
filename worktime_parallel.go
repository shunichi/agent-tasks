package main

import (
	"cmp"
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

var parallelTemplate = template.Must(template.New("parallel").Parse(parallelHTML))

// parallelHTML は自己完結ページ。Go の raw 文字列 (backtick) なので、埋め込む JS では
// テンプレートリテラル (backtick) を使わず文字列連結 (+) で組む (backtick 衝突を避けるため)。
const parallelHTML = `<!doctype html>
<html lang="ja">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>agent-tasks — 時間帯・並列稼働</title>
<style>
  :root {
    --bg:#0f1115; --panel:#171a21; --card:#1d222b; --card2:#232936; --border:#2a2f3a;
    /* dim/dim2 は補助テキスト (dim=範囲表示/凡例/所要, dim2=軸ラベル/ヒント/見出し)。小さめ・淡い文字が
       多いので WCAG AAA 目安 (7:1) 以上まで濃くする (dim #b3bac6=9.7:1 / dim2 #9aa2b1=7.4:1。bg 比)。
       これより暗くすると軸ラベル等が潰れる (0133)。fg>dim>dim2 の階層は維持。 */
    --fg:#e6e8ec; --dim:#b3bac6; --dim2:#9aa2b1; --accent:#4a9eff; --id:#ffd479;
    --mono: ui-monospace, "SFMono-Regular", "Menlo", "Consolas", monospace;
    --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", "Hiragino Sans", "Noto Sans JP", sans-serif;
  }
  * { box-sizing:border-box; }
  body { margin:0; padding:0 0 3rem; background:var(--bg); color:var(--fg); font-family:var(--sans);
         font-size:15px; line-height:1.5; -webkit-font-smoothing:antialiased; }
  header { position:sticky; top:0; z-index:10; background:var(--panel);
           border-bottom:1px solid var(--border); padding:.7rem 1rem; }
  header h1 { margin:0; font-size:1.1rem; display:flex; align-items:center; gap:.6rem; flex-wrap:wrap; }
  header h1 a { color:var(--accent); text-decoration:none; font-size:.85rem; font-weight:400; }
  header .meta { color:var(--dim); font-size:.78rem; margin-top:.15rem;
                 font-family:var(--mono); font-variant-numeric:tabular-nums; }
  main { padding:1rem 1.1rem; max-width:1060px; margin:0 auto; }

  #pcwarn { display:none; margin:0 1rem 0; background:#2a1e12; border:1px solid #4a3520;
            color:#f0a868; font-size:.8rem; padding:.5rem .75rem; border-radius:.4rem; }

  h2.sec { font-size:.74rem; font-family:var(--mono); letter-spacing:.12em; text-transform:uppercase;
           color:var(--dim2); margin:1.6rem 0 .7rem; display:flex; align-items:center; gap:.7rem; }
  h2.sec:first-of-type { margin-top:.2rem; }
  h2.sec::after { content:""; flex:1; height:1px; background:var(--border); }
  .panelbox { background:var(--panel); border:1px solid var(--border); border-radius:.7rem;
              padding:.9rem 1rem 1rem; overflow-x:auto; }
  .capt { font-family:var(--mono); font-size:.73rem; color:var(--dim); display:flex;
          justify-content:space-between; margin-bottom:.6rem; gap:1rem; flex-wrap:wrap; }
  .capt .badge { color:var(--dim2); }
  .lanenav { display:flex; gap:.4rem; align-items:center; margin:0 0 .7rem; flex-wrap:wrap; }
  .lnav { background:var(--card); border:1px solid var(--border); color:var(--fg);
          font-family:var(--sans); font-size:.78rem; cursor:pointer; padding:.3rem .7rem; border-radius:.4rem; }
  .lnav:hover:not([disabled]) { border-color:var(--accent); color:var(--accent); }
  .lnav[disabled] { opacity:.4; cursor:default; }
  .lnav.on { background:var(--accent); color:#08111f; border-color:var(--accent); font-weight:600; }

  .plegend { display:flex; flex-wrap:wrap; gap:.3rem .9rem; margin:.8rem 0 0;
             font-size:.72rem; font-family:var(--mono); }
  .plegend .it { display:inline-flex; align-items:center; gap:.34rem; color:var(--dim); }
  .plegend .sw { width:.7rem; height:.7rem; border-radius:2px; flex:none; }

  .axis-lab { fill:var(--dim2); font-family:var(--mono); font-size:10.5px; font-weight:500; }
  .grid-line { stroke:var(--border); stroke-width:1; }
  .cday { cursor:pointer; }

  /* 詳細ビュー */
  .d-back { background:var(--card); border:1px solid var(--border); color:var(--accent);
            font-family:var(--sans); font-size:.82rem; cursor:pointer; padding:.32rem .7rem;
            border-radius:.4rem; margin-bottom:.9rem; }
  .d-back:hover { border-color:var(--accent); }
  .d-title { display:flex; align-items:baseline; gap:1.1rem; flex-wrap:wrap; margin-bottom:.8rem; }
  .d-title .d-date { font-size:1.02rem; font-weight:700; }
  .d-title .d-stat { font-family:var(--mono); font-size:.78rem; color:var(--dim); }
  .d-title .d-stat b { color:var(--fg); font-weight:700; }
  .d-fold { font-family:var(--mono); font-size:.75rem; color:var(--dim2); margin-top:.5rem; }
  .lane-id { fill:var(--id); font-family:var(--mono); font-size:10px; font-weight:700; }
  .lane-ti { fill:var(--fg); font-family:var(--sans); font-size:10.5px; }
  .lane-dr { fill:var(--dim); font-family:var(--mono); font-size:10px; }

  .empty { color:var(--dim); text-align:center; margin-top:3rem; }
  svg { display:block; }
</style>
</head>
<body>
<header>
  <h1>時間帯・並列稼働 <a href="/">← 一覧</a> <a href="/worktime">← 時間配分</a></h1>
  <div class="meta" id="meta"></div>
</header>
<div id="pcwarn">このビューは横に広い時刻軸が要るため <b>PC 向け</b>です。画面が狭いと帯が細くなり読みにくくなります (横スクロールで見られます)。</div>
<main id="app"></main>
{{if .HasData}}
<script>
(function(){
  var PIECES   = {{.PiecesJSON}};   // [{p,id,ti,d,s,e}] s/e = 当日0:00からの分
  var COLORS   = {{.ColorsJSON}};   // project -> css color
  // このビューは自動更新しない (0134)。ロード時のスナップショットを表示し、最新が要るときは手動リロード。
  var app = document.getElementById("app");
  var metaEl = document.getElementById("meta");
  var WD = ["月","火","水","木","金","土","日"];  // 0=月
  var PAGE_DAYS = 14;   // スイムレーンの 1 ページ日数 (この単位で過去へページング)
  var MAX_LANE  = 12;   // 詳細ビューのレーン上限 (超過分は畳む)
  var W = 980, PADL = 52, PADR = 10;

  var ESC = {"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;"};
  function esc(s){ return String(s).replace(/[&<>"]/g, function(c){ return ESC[c]; }); }
  function colorOf(p){ return COLORS[p] || "#888"; }
  function clip(s,n){ s=String(s); return s.length>n ? s.slice(0,n-1)+"…" : s; }
  function hm(mins){ mins=Math.round(mins); if(mins<60) return mins+"m";
    var h=Math.floor(mins/60), mm=mins%60; return mm ? h+"h"+mm+"m" : h+"h"; }
  function fmtT(min){ var h=Math.floor(min/60), m=min%60; return (h<10?"0":"")+h+":"+(m<10?"0":"")+m; }
  function pad2(n){ return String(n).padStart(2,"0"); }
  function parseD(s){ var a=s.split("-"); return new Date(+a[0], +a[1]-1, +a[2]); }
  function dowOf(dstr){ return (parseD(dstr).getDay()+6)%7; } // 0=月
  function dlabel(dstr){ var d=parseD(dstr); return (d.getMonth()+1)+"/"+d.getDate()+" ("+WD[dowOf(dstr)]+")"; }
  function xOf(min, w){ return PADL + (min/1440)*(w-PADL-PADR); }
  function hourTicks(){ var o=[]; for(var h=0;h<=24;h+=3) o.push(h); return o; }

  // day: 詳細で開いている日。pageStart: スイムレーン表示窓の先頭 index (0=最新)。
  // showAll: 全期間 (窓を使わず全日表示) トグル。
  var state = { day:null, pageStart:0, showAll:false };

  // 日ごとに piece をまとめる
  function byDate(){
    var m={}; PIECES.forEach(function(p){ (m[p.d]=m[p.d]||[]).push(p); }); return m;
  }
  function datesDesc(m){ return Object.keys(m).sort().reverse(); }
  function concAt(list, min){ var c=0; for(var i=0;i<list.length;i++){ if(list[i].s<=min && min<list[i].e) c++; } return c; }

  function render(){
    var m = byDate();
    var dates = datesDesc(m);
    // meta
    var tot=0; PIECES.forEach(function(p){ tot += (p.e-p.s); });
    metaEl.textContent = dates.length+" 日 · 稼働合計 "+hm(tot);
    // 選択日の維持 (消えていたら一番活動の多い日)
    if(!state.day || !m[state.day]) state.day = mostActive(m, dates);

    app.innerHTML =
      '<h2 class="sec">典型的な1週間 — 曜日 × 時刻ヒートマップ</h2>'+
      '<div class="panelbox"><div class="capt"><span>全期間の稼働を曜日×時刻で重ね合わせ</span>'+
        '<span class="badge">濃さ = 平均並列度</span></div><div id="heat"></div></div>'+
      '<h2 class="sec">日別 — 0–24h スイムレーン</h2>'+
      '<div class="panelbox"><div class="capt"><span id="laneRange"></span>'+
        '<span class="badge">重なり = 並列度 · 色 = プロジェクト · 日をクリックで詳細 ↓</span></div>'+
        '<div class="lanenav" id="lanenav"></div>'+
        '<div id="lanes"></div><div class="plegend" id="legend"></div></div>'+
      '<h2 class="sec">1日の詳細 — タスク別レーン</h2>'+
      '<div class="panelbox"><div class="capt"><span>上 = 並列度 / 下 = タスク別レーン</span>'+
        '<span class="badge">レーン = タスク · 色 = プロジェクト</span></div><div id="detail"></div></div>';

    renderHeatmap(m, dates);
    renderLanes(m, dates);
    renderLegend();
    renderDetail(state.day);
    pcGuard();
  }

  function mostActive(m, dates){
    var best=dates[0], bestN=-1;
    dates.forEach(function(d){
      var n = {}; m[d].forEach(function(p){ n[p.id]=1; });
      var c = Object.keys(n).length;
      if(c>bestN){ bestN=c; best=d; }
    });
    return best;
  }

  // ---- ヒートマップ (曜日 × 時刻) ----
  function renderHeatmap(m, dates){
    var grid=[], dowDays=new Array(7).fill(0);
    for(var r=0;r<7;r++) grid[r]=new Array(24).fill(0);
    dates.forEach(function(d){
      var dw=dowOf(d); dowDays[dw]++;
      for(var h=0;h<24;h++) grid[dw][h] += concAt(m[d], h*60+30);
    });
    for(var r=0;r<7;r++) for(var h=0;h<24;h++) grid[r][h] = dowDays[r] ? grid[r][h]/dowDays[r] : 0;
    var mx=0.0001; for(var r=0;r<7;r++) for(var h=0;h<24;h++) if(grid[r][h]>mx) mx=grid[r][h];

    var cell=30, gap=3, labW=30, topH=16;
    var Wb=labW+24*(cell+gap), Hb=topH+7*(cell+gap);
    var svg=['<svg viewBox="0 0 '+Wb+' '+Hb+'" width="100%" preserveAspectRatio="xMidYMid meet" role="img" aria-label="曜日×時刻ヒートマップ">'];
    for(var h=0;h<24;h+=3){ var x=labW+h*(cell+gap)+cell/2;
      svg.push('<text class="axis-lab" x="'+x+'" y="11" text-anchor="middle">'+h+'</text>'); }
    for(var r=0;r<7;r++){
      var y=topH+r*(cell+gap);
      svg.push('<text class="axis-lab" x="'+(labW-8)+'" y="'+(y+cell/2+3)+'" text-anchor="end">'+WD[r]+'</text>');
      for(var h=0;h<24;h++){
        var x=labW+h*(cell+gap), v=grid[r][h]/mx;
        var op = v<=0 ? 0.04 : (0.12+v*0.85);
        var fill = v<=0 ? "var(--card)" : "var(--accent)";
        svg.push('<rect x="'+x+'" y="'+y+'" width="'+cell+'" height="'+cell+'" rx="4" fill="'+fill+'" fill-opacity="'+op.toFixed(3)+'"><title>'+WD[r]+' '+h+':00 · 平均並列 '+grid[r][h].toFixed(2)+'</title></rect>');
      }
    }
    svg.push('</svg>');
    document.getElementById("heat").innerHTML = svg.join("");
  }

  // ---- 日別スイムレーン (ページング + 全期間トグル) ----
  function renderLanes(m, dates){
    var total = dates.length;
    // 表示窓を決める。全期間トグル時は全日、そうでなければ pageStart から PAGE_DAYS 日分。
    var maxStart = Math.max(0, total - PAGE_DAYS);
    if(state.pageStart > maxStart) state.pageStart = maxStart;
    if(state.pageStart < 0) state.pageStart = 0;
    var show, winStart, winEnd;
    if(state.showAll){
      show = dates; winStart = 0; winEnd = total;
    } else {
      winStart = state.pageStart; winEnd = Math.min(total, winStart+PAGE_DAYS);
      show = dates.slice(winStart, winEnd);
    }
    renderLaneNav(dates, total, winStart, winEnd);

    var rowH=22, gap=6, topH=16;
    var Hc=topH+show.length*(rowH+gap)+6;
    var svg=['<svg viewBox="0 0 '+W+' '+Hc+'" width="100%" preserveAspectRatio="xMidYMid meet" role="img" aria-label="日別スイムレーン">'];
    hourTicks().forEach(function(h){ var x=xOf(h*60,W);
      svg.push('<line class="grid-line" x1="'+x+'" y1="'+topH+'" x2="'+x+'" y2="'+(Hc-4)+'" opacity="0.4"/>');
      svg.push('<text class="axis-lab" x="'+x+'" y="11" text-anchor="middle">'+h+'</text>'); });
    show.forEach(function(d,k){
      var y=topH+k*(rowH+gap), sel=(d===state.day);
      svg.push('<g class="cday" data-day="'+d+'">');
      svg.push('<rect class="cday-bg" x="'+PADL+'" y="'+y+'" width="'+(W-PADL-PADR)+'" height="'+rowH+'" rx="4" fill="var(--card)" fill-opacity="'+(sel?"0.85":"0.5")+'" stroke="'+(sel?"var(--accent)":"none")+'" stroke-width="'+(sel?"1.5":"0")+'"/>');
      svg.push('<text class="axis-lab" x="'+(PADL-6)+'" y="'+(y+rowH/2+3)+'" text-anchor="end">'+esc(dlabel(d))+'</text>');
      m[d].forEach(function(p){
        var x1=xOf(p.s,W), x2=xOf(p.e,W), w=Math.max(1.5,x2-x1);
        svg.push('<rect x="'+x1.toFixed(1)+'" y="'+(y+3)+'" width="'+w.toFixed(1)+'" height="'+(rowH-6)+'" rx="2.5" fill="'+colorOf(p.p)+'" fill-opacity="0.5"><title>#'+esc(p.id)+' '+esc(p.ti)+'</title></rect>');
      });
      svg.push('</g>');
    });
    svg.push('</svg>');
    var host=document.getElementById("lanes");
    host.innerHTML=svg.join("");
    Array.prototype.forEach.call(host.querySelectorAll(".cday"), function(g){
      g.addEventListener("click", function(){ state.day=g.dataset.day; render();
        document.getElementById("detail").scrollIntoView({behavior:"smooth", block:"nearest"}); });
    });
  }

  // 表示窓のページング操作 + 現在範囲ラベル。全 piece はクライアントに埋め込み済みなので
  // ページングはサーバ往復なしで完結する (古い日も遡れる。全期間トグルで一気に全日表示)。
  function renderLaneNav(dates, total, winStart, winEnd){
    var rangeEl=document.getElementById("laneRange"), navEl=document.getElementById("lanenav");
    if(!rangeEl || !navEl) return;
    if(total===0){ rangeEl.textContent="データなし"; navEl.innerHTML=""; return; }
    if(state.showAll){
      rangeEl.textContent="全期間 "+total+" 日 ("+esc(dlabel(dates[total-1]))+" 〜 "+esc(dlabel(dates[0]))+")";
    } else {
      rangeEl.textContent=esc(dlabel(dates[winEnd-1]))+" 〜 "+esc(dlabel(dates[winStart]))+
        " · 全 "+total+" 日中 "+(winStart+1)+"–"+winEnd+" 日目";
    }
    var canOlder = !state.showAll && winEnd < total;
    var canNewer = !state.showAll && winStart > 0;
    navEl.innerHTML =
      '<button class="lnav" id="lnOlder"'+(canOlder?"":" disabled")+'>← 古い'+PAGE_DAYS+'日</button>'+
      '<button class="lnav" id="lnNewer"'+(canNewer?"":" disabled")+'>新しい'+PAGE_DAYS+'日 →</button>'+
      '<button class="lnav'+(state.showAll?" on":"")+'" id="lnAll">'+(state.showAll?"直近だけに戻す":"全期間 ("+total+"日)")+'</button>';
    var older=document.getElementById("lnOlder"), newer=document.getElementById("lnNewer"), all=document.getElementById("lnAll");
    if(older) older.onclick=function(){ if(canOlder){ state.pageStart+=PAGE_DAYS; render(); } };
    if(newer) newer.onclick=function(){ if(canNewer){ state.pageStart=Math.max(0,state.pageStart-PAGE_DAYS); render(); } };
    if(all)   all.onclick=function(){ state.showAll=!state.showAll; if(!state.showAll) state.pageStart=0; render(); };
  }

  function renderLegend(){
    var names=Object.keys(COLORS).sort();
    document.getElementById("legend").innerHTML = names.map(function(p){
      return '<span class="it"><span class="sw" style="background:'+colorOf(p)+'"></span>'+esc(p)+'</span>';
    }).join("") + '<span class="it" style="color:var(--dim2)">重なるほど濃い = 並列</span>';
  }

  // ---- 1日の詳細 (タスク別レーン + 並列度ストリップ) ----
  function renderDetail(d){
    var m=byDate(); var day=m[d]||[];
    // タスク単位に集約
    var byTask={};
    day.forEach(function(p){
      var k=p.id;
      if(!byTask[k]) byTask[k]={id:p.id, ti:p.ti, proj:p.p, ivs:[], total:0, first:p.s};
      byTask[k].ivs.push(p); byTask[k].total += (p.e-p.s); byTask[k].first=Math.min(byTask[k].first,p.s);
    });
    var all=Object.keys(byTask).map(function(k){ return byTask[k]; });
    // 上限超過は合計降順で上位を採用、残りは畳む
    var folded=0, foldedSec=0;
    if(all.length>MAX_LANE){
      all.sort(function(a,b){ return b.total-a.total; });
      var keep=all.slice(0,MAX_LANE), rest=all.slice(MAX_LANE);
      folded=rest.length; rest.forEach(function(t){ foldedSec+=t.total; });
      all=keep;
    }
    all.sort(function(a,b){ return a.first-b.first; }); // 開始が早い順 (ガント風)

    var dayTot=0; day.forEach(function(p){ dayTot+=(p.e-p.s); });
    var maxC=1; for(var mn=0;mn<1440;mn+=5){ var c=concAt(day,mn); if(c>maxC)maxC=c; }

    var head='<button class="d-back" id="dBack">↑ 一覧の日を選び直す</button>'+
      '<div class="d-title"><span class="d-date">'+esc(dlabel(d))+' の詳細</span>'+
      '<span class="d-stat">稼働 <b>'+hm(dayTot)+'</b></span>'+
      '<span class="d-stat">最大並列 <b>'+maxC+'</b></span>'+
      '<span class="d-stat">タスク <b>'+Object.keys(byTask).length+'</b></span></div>';

    var stripH=42, laneH=26, laneGap=6, topH=16, labW2=232, Wd=980;
    function xd(min){ return labW2+(min/1440)*(Wd-labW2-8); }
    var Hd=topH+stripH+8+all.length*(laneH+laneGap)+6;
    var svg=['<svg viewBox="0 0 '+Wd+' '+Hd+'" width="100%" preserveAspectRatio="xMidYMid meet" role="img" aria-label="1日の詳細タスク別レーン">'];
    for(var h=0;h<=24;h+=3){ var x=xd(h*60);
      svg.push('<line class="grid-line" x1="'+x+'" y1="'+topH+'" x2="'+x+'" y2="'+(Hd-4)+'" opacity="0.35"/>');
      svg.push('<text class="axis-lab" x="'+x+'" y="11" text-anchor="middle">'+h+'</text>'); }
    // 並列度ストリップ
    var sy0=topH, sy1=topH+stripH;
    svg.push('<rect x="'+labW2+'" y="'+sy0+'" width="'+(Wd-labW2-8)+'" height="'+stripH+'" fill="var(--card)" fill-opacity="0.4" rx="4"/>');
    svg.push('<text class="axis-lab" x="'+(labW2-8)+'" y="'+(sy0+12)+'" text-anchor="end">並列度</text>');
    svg.push('<text class="axis-lab" x="'+(labW2-8)+'" y="'+(sy1-3)+'" text-anchor="end">'+maxC+'</text>');
    var pts=[labW2+","+sy1];
    for(var mn=0;mn<=1440;mn+=15){ pts.push(xd(mn)+","+(sy1-(concAt(day,mn)/maxC)*(stripH-4))); }
    pts.push((Wd-8)+","+sy1);
    svg.push('<polygon points="'+pts.join(" ")+'" fill="var(--accent)" fill-opacity="0.28" stroke="var(--accent)" stroke-opacity="0.6" stroke-width="1"/>');
    // タスク別レーン
    all.forEach(function(tk,i){
      var y=topH+stripH+8+i*(laneH+laneGap);
      svg.push('<rect x="'+labW2+'" y="'+y+'" width="'+(Wd-labW2-8)+'" height="'+laneH+'" rx="4" fill="var(--card)" fill-opacity="0.45"/>');
      svg.push('<rect x="0" y="'+(y+laneH/2-5)+'" width="10" height="10" rx="2" fill="'+colorOf(tk.proj)+'"/>');
      svg.push('<text class="lane-id" x="16" y="'+(y+laneH/2+3)+'">#'+esc(tk.id)+'</text>');
      svg.push('<text class="lane-ti" x="58" y="'+(y+laneH/2+3)+'">'+esc(clip(tk.ti,16))+'</text>');
      svg.push('<text class="lane-dr" x="'+(labW2-8)+'" y="'+(y+laneH/2+3)+'" text-anchor="end">'+hm(tk.total)+'</text>');
      tk.ivs.forEach(function(iv){
        var x1=xd(iv.s), x2=xd(iv.e), w=Math.max(2,x2-x1);
        svg.push('<rect x="'+x1.toFixed(1)+'" y="'+(y+4)+'" width="'+w.toFixed(1)+'" height="'+(laneH-8)+'" rx="2.5" fill="'+colorOf(tk.proj)+'" fill-opacity="0.9"><title>#'+esc(tk.id)+' '+esc(tk.ti)+' '+fmtT(iv.s)+'–'+fmtT(iv.e)+'</title></rect>');
      });
    });
    svg.push('</svg>');
    var foldNote = folded>0 ? '<div class="d-fold">＋ 他 '+folded+' タスク (合計 '+hm(foldedSec)+') を省略 (稼働の多い上位 '+MAX_LANE+' 件を表示)</div>' : '';
    var box=document.getElementById("detail");
    box.innerHTML = head + svg.join("") + foldNote;
    document.getElementById("dBack").addEventListener("click", function(){
      document.getElementById("lanes").scrollIntoView({behavior:"smooth", block:"center"}); });
  }

  function pcGuard(){
    document.getElementById("pcwarn").style.display = window.innerWidth < 820 ? "block" : "none";
  }
  window.addEventListener("resize", pcGuard);

  // このビューは自動更新しない (0134)。ロード時に一度だけ描画する。最新データが要るときは手動リロード。
  render();
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
