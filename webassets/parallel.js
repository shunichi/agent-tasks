"use strict";
// 並列稼働ビューのクライアントロジック。
//
// このファイルは worktime_parallel.go から //go:embed でバイナリに取り込まれ、ページの <script> に
// そのままインライン展開される (単一の自己完結ページを維持)。ブラウザではデータは bootstrap script が
// window.PIECES / window.COLORS に注入し、末尾のガードが startParallel() を起動する。
// 純粋関数 (整形・日付・並列度) はモジュール先頭に切り出してあり、Node (vitest) から module.exports
// 経由でテストできる。document/window の無い Node では描画コードは走らない。
//
// 注意: このファイルは <script> (非 module) としてインライン展開されるため、ESM の import/export は
// 使わない (構文エラーになる)。データ受け渡しはテンプレートリテラル (backtick) を避け文字列連結 (+) で組む。

var WD = ["月", "火", "水", "木", "金", "土", "日"]; // 0=月

var ESC = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" };
function esc(s) { return String(s).replace(/[&<>"]/g, function (c) { return ESC[c]; }); }
function clip(s, n) { s = String(s); return s.length > n ? s.slice(0, n - 1) + "…" : s; }
function hm(mins) {
  mins = Math.round(mins); if (mins < 60) return mins + "m";
  var h = Math.floor(mins / 60), mm = mins % 60; return mm ? h + "h" + mm + "m" : h + "h";
}
function fmtT(min) { var h = Math.floor(min / 60), m = min % 60; return (h < 10 ? "0" : "") + h + ":" + (m < 10 ? "0" : "") + m; }
function pad2(n) { return String(n).padStart(2, "0"); }
function parseD(s) { var a = s.split("-"); return new Date(+a[0], +a[1] - 1, +a[2]); }
function dowOf(dstr) { return (parseD(dstr).getDay() + 6) % 7; } // 0=月
function dlabel(dstr) { var d = parseD(dstr); return (d.getMonth() + 1) + "/" + d.getDate() + " (" + WD[dowOf(dstr)] + ")"; }

// 指定分 (当日 0:00 からの分) に稼働中だった piece 数 = 並列度
function concAt(list, min) { var c = 0; for (var i = 0; i < list.length; i++) { if (list[i].s <= min && min < list[i].e) c++; } return c; }
function datesDesc(m) { return Object.keys(m).sort().reverse(); }
// pieces を日ごとにまとめる
function byDate(pieces) { var m = {}; pieces.forEach(function (p) { (m[p.d] = m[p.d] || []).push(p); }); return m; }
function mostActive(m, dates) {
  var best = dates[0], bestN = -1;
  dates.forEach(function (d) {
    var n = {}; m[d].forEach(function (p) { n[p.id] = 1; });
    var c = Object.keys(n).length;
    if (c > bestN) { bestN = c; best = d; }
  });
  return best;
}

// ---- DOM 描画 (ブラウザのみ) ----
function startParallel(PIECES, COLORS) {
  var app = document.getElementById("app");
  var metaEl = document.getElementById("meta");
  var PAGE_DAYS = 14;   // スイムレーンの 1 ページ日数 (この単位で過去へページング)
  var MAX_LANE = 12;    // 詳細ビューのレーン上限 (超過分は畳む)
  var W = 980, PADL = 52, PADR = 10;

  function colorOf(p) { return COLORS[p] || "#888"; }
  function xOf(min, w) { return PADL + (min / 1440) * (w - PADL - PADR); }
  function hourTicks() { var o = []; for (var h = 0; h <= 24; h += 3) o.push(h); return o; }

  // day: 詳細で開いている日。pageStart: スイムレーン表示窓の先頭 index (0=最新)。
  // showAll: 全期間 (窓を使わず全日表示) トグル。
  var state = { day: null, pageStart: 0, showAll: false };

  function render() {
    var m = byDate(PIECES);
    var dates = datesDesc(m);
    // meta
    var tot = 0; PIECES.forEach(function (p) { tot += (p.e - p.s); });
    metaEl.textContent = dates.length + " 日 · 稼働合計 " + hm(tot);
    // 選択日の維持 (消えていたら一番活動の多い日)
    if (!state.day || !m[state.day]) state.day = mostActive(m, dates);

    app.innerHTML =
      '<h2 class="sec">典型的な1週間 — 曜日 × 時刻ヒートマップ</h2>' +
      '<div class="panelbox"><div class="capt"><span>全期間の稼働を曜日×時刻で重ね合わせ</span>' +
        '<span class="badge">濃さ = 平均並列度</span></div><div id="heat"></div></div>' +
      '<h2 class="sec">日別 — 0–24h スイムレーン</h2>' +
      '<div class="panelbox"><div class="capt"><span id="laneRange"></span>' +
        '<span class="badge">重なり = 並列度 · 色 = プロジェクト · 日をクリックで詳細 ↓</span></div>' +
        '<div class="lanenav" id="lanenav"></div>' +
        '<div id="lanes"></div><div class="plegend" id="legend"></div></div>' +
      '<h2 class="sec">1日の詳細 — タスク別レーン</h2>' +
      '<div class="panelbox"><div class="capt"><span>上 = 並列度 / 下 = タスク別レーン</span>' +
        '<span class="badge">レーン = タスク · 色 = プロジェクト</span></div><div id="detail"></div></div>';

    renderHeatmap(m, dates);
    renderLanes(m, dates);
    renderLegend();
    renderDetail(state.day);
    pcGuard();
  }

  // ---- ヒートマップ (曜日 × 時刻) ----
  function renderHeatmap(m, dates) {
    var grid = [], dowDays = new Array(7).fill(0);
    for (var r = 0; r < 7; r++) grid[r] = new Array(24).fill(0);
    dates.forEach(function (d) {
      var dw = dowOf(d); dowDays[dw]++;
      for (var h = 0; h < 24; h++) grid[dw][h] += concAt(m[d], h * 60 + 30);
    });
    for (var r = 0; r < 7; r++) for (var h = 0; h < 24; h++) grid[r][h] = dowDays[r] ? grid[r][h] / dowDays[r] : 0;
    var mx = 0.0001; for (var r = 0; r < 7; r++) for (var h = 0; h < 24; h++) if (grid[r][h] > mx) mx = grid[r][h];

    var cell = 30, gap = 3, labW = 30, topH = 16;
    var Wb = labW + 24 * (cell + gap), Hb = topH + 7 * (cell + gap);
    var svg = ['<svg viewBox="0 0 ' + Wb + ' ' + Hb + '" width="100%" preserveAspectRatio="xMidYMid meet" role="img" aria-label="曜日×時刻ヒートマップ">'];
    for (var h = 0; h < 24; h += 3) {
      var x = labW + h * (cell + gap) + cell / 2;
      svg.push('<text class="axis-lab" x="' + x + '" y="11" text-anchor="middle">' + h + '</text>');
    }
    for (var r = 0; r < 7; r++) {
      var y = topH + r * (cell + gap);
      svg.push('<text class="axis-lab" x="' + (labW - 8) + '" y="' + (y + cell / 2 + 3) + '" text-anchor="end">' + WD[r] + '</text>');
      for (var h = 0; h < 24; h++) {
        var x = labW + h * (cell + gap), v = grid[r][h] / mx;
        var op = v <= 0 ? 0.04 : (0.12 + v * 0.85);
        var fill = v <= 0 ? "var(--card)" : "var(--accent)";
        var tip = esc(WD[r] + " " + h + ":00 · 平均並列 " + grid[r][h].toFixed(2));
        svg.push('<rect x="' + x + '" y="' + y + '" width="' + cell + '" height="' + cell + '" rx="4" fill="' + fill + '" fill-opacity="' + op.toFixed(3) + '" data-tip="' + tip + '" aria-label="' + tip + '"/>');
      }
    }
    svg.push('</svg>');
    document.getElementById("heat").innerHTML = svg.join("");
  }

  // ---- 日別スイムレーン (ページング + 全期間トグル) ----
  function renderLanes(m, dates) {
    var total = dates.length;
    // 表示窓を決める。全期間トグル時は全日、そうでなければ pageStart から PAGE_DAYS 日分。
    var maxStart = Math.max(0, total - PAGE_DAYS);
    if (state.pageStart > maxStart) state.pageStart = maxStart;
    if (state.pageStart < 0) state.pageStart = 0;
    var show, winStart, winEnd;
    if (state.showAll) {
      show = dates; winStart = 0; winEnd = total;
    } else {
      winStart = state.pageStart; winEnd = Math.min(total, winStart + PAGE_DAYS);
      show = dates.slice(winStart, winEnd);
    }
    renderLaneNav(dates, total, winStart, winEnd);

    var rowH = 22, gap = 6, topH = 16;
    var Hc = topH + show.length * (rowH + gap) + 6;
    var svg = ['<svg viewBox="0 0 ' + W + ' ' + Hc + '" width="100%" preserveAspectRatio="xMidYMid meet" role="img" aria-label="日別スイムレーン">'];
    hourTicks().forEach(function (h) {
      var x = xOf(h * 60, W);
      svg.push('<line class="grid-line" x1="' + x + '" y1="' + topH + '" x2="' + x + '" y2="' + (Hc - 4) + '" opacity="0.4"/>');
      svg.push('<text class="axis-lab" x="' + x + '" y="11" text-anchor="middle">' + h + '</text>');
    });
    show.forEach(function (d, k) {
      var y = topH + k * (rowH + gap), sel = (d === state.day);
      svg.push('<g class="cday" data-day="' + d + '">');
      svg.push('<rect class="cday-bg" x="' + PADL + '" y="' + y + '" width="' + (W - PADL - PADR) + '" height="' + rowH + '" rx="4" fill="var(--card)" fill-opacity="' + (sel ? "0.85" : "0.5") + '" stroke="' + (sel ? "var(--accent)" : "none") + '" stroke-width="' + (sel ? "1.5" : "0") + '"/>');
      svg.push('<text class="axis-lab" x="' + (PADL - 6) + '" y="' + (y + rowH / 2 + 3) + '" text-anchor="end">' + esc(dlabel(d)) + '</text>');
      m[d].forEach(function (p) {
        var x1 = xOf(p.s, W), x2 = xOf(p.e, W), w = Math.max(1.5, x2 - x1);
        var tip = "#" + esc(p.id) + " " + esc(p.ti);
        svg.push('<rect x="' + x1.toFixed(1) + '" y="' + (y + 3) + '" width="' + w.toFixed(1) + '" height="' + (rowH - 6) + '" rx="2.5" fill="' + colorOf(p.p) + '" fill-opacity="0.5" data-tip="' + tip + '" aria-label="' + tip + '"/>');
      });
      svg.push('</g>');
    });
    svg.push('</svg>');
    var host = document.getElementById("lanes");
    host.innerHTML = svg.join("");
    Array.prototype.forEach.call(host.querySelectorAll(".cday"), function (g) {
      g.addEventListener("click", function () {
        state.day = g.dataset.day; render();
        document.getElementById("detail").scrollIntoView({ behavior: "smooth", block: "nearest" });
      });
    });
  }

  // 表示窓のページング操作 + 現在範囲ラベル。全 piece はクライアントに埋め込み済みなので
  // ページングはサーバ往復なしで完結する (古い日も遡れる。全期間トグルで一気に全日表示)。
  function renderLaneNav(dates, total, winStart, winEnd) {
    var rangeEl = document.getElementById("laneRange"), navEl = document.getElementById("lanenav");
    if (!rangeEl || !navEl) return;
    if (total === 0) { rangeEl.textContent = "データなし"; navEl.innerHTML = ""; return; }
    if (state.showAll) {
      rangeEl.textContent = "全期間 " + total + " 日 (" + esc(dlabel(dates[total - 1])) + " 〜 " + esc(dlabel(dates[0])) + ")";
    } else {
      rangeEl.textContent = esc(dlabel(dates[winEnd - 1])) + " 〜 " + esc(dlabel(dates[winStart])) +
        " · 全 " + total + " 日中 " + (winStart + 1) + "–" + winEnd + " 日目";
    }
    var canOlder = !state.showAll && winEnd < total;
    var canNewer = !state.showAll && winStart > 0;
    navEl.innerHTML =
      '<button class="lnav" id="lnOlder"' + (canOlder ? "" : " disabled") + '>← 古い' + PAGE_DAYS + '日</button>' +
      '<button class="lnav" id="lnNewer"' + (canNewer ? "" : " disabled") + '>新しい' + PAGE_DAYS + '日 →</button>' +
      '<button class="lnav' + (state.showAll ? " on" : "") + '" id="lnAll">' + (state.showAll ? "直近だけに戻す" : "全期間 (" + total + "日)") + '</button>';
    var older = document.getElementById("lnOlder"), newer = document.getElementById("lnNewer"), all = document.getElementById("lnAll");
    if (older) older.onclick = function () { if (canOlder) { state.pageStart += PAGE_DAYS; render(); } };
    if (newer) newer.onclick = function () { if (canNewer) { state.pageStart = Math.max(0, state.pageStart - PAGE_DAYS); render(); } };
    if (all) all.onclick = function () { state.showAll = !state.showAll; if (!state.showAll) state.pageStart = 0; render(); };
  }

  function renderLegend() {
    var names = Object.keys(COLORS).sort();
    document.getElementById("legend").innerHTML = names.map(function (p) {
      return '<span class="it"><span class="sw" style="background:' + colorOf(p) + '"></span>' + esc(p) + '</span>';
    }).join("") + '<span class="it" style="color:var(--dim2)">重なるほど濃い = 並列</span>';
  }

  // ---- 1日の詳細 (タスク別レーン + 並列度ストリップ) ----
  function renderDetail(d) {
    var m = byDate(PIECES); var day = m[d] || [];
    // タスク単位に集約
    var byTask = {};
    day.forEach(function (p) {
      var k = p.id;
      if (!byTask[k]) byTask[k] = { id: p.id, ti: p.ti, proj: p.p, ivs: [], total: 0, first: p.s };
      byTask[k].ivs.push(p); byTask[k].total += (p.e - p.s); byTask[k].first = Math.min(byTask[k].first, p.s);
    });
    var all = Object.keys(byTask).map(function (k) { return byTask[k]; });
    // 上限超過は合計降順で上位を採用、残りは畳む
    var folded = 0, foldedSec = 0;
    if (all.length > MAX_LANE) {
      all.sort(function (a, b) { return b.total - a.total; });
      var keep = all.slice(0, MAX_LANE), rest = all.slice(MAX_LANE);
      folded = rest.length; rest.forEach(function (t) { foldedSec += t.total; });
      all = keep;
    }
    all.sort(function (a, b) { return a.first - b.first; }); // 開始が早い順 (ガント風)

    var dayTot = 0; day.forEach(function (p) { dayTot += (p.e - p.s); });
    var maxC = 1; for (var mn = 0; mn < 1440; mn += 5) { var c = concAt(day, mn); if (c > maxC) maxC = c; }

    var head = '<button class="d-back" id="dBack">↑ 一覧の日を選び直す</button>' +
      '<div class="d-title"><span class="d-date">' + esc(dlabel(d)) + ' の詳細</span>' +
      '<span class="d-stat">稼働 <b>' + hm(dayTot) + '</b></span>' +
      '<span class="d-stat">最大並列 <b>' + maxC + '</b></span>' +
      '<span class="d-stat">タスク <b>' + Object.keys(byTask).length + '</b></span></div>';

    var stripH = 42, laneH = 26, laneGap = 6, topH = 16, labW2 = 232, Wd = 980;
    function xd(min) { return labW2 + (min / 1440) * (Wd - labW2 - 8); }
    var Hd = topH + stripH + 8 + all.length * (laneH + laneGap) + 6;
    var svg = ['<svg viewBox="0 0 ' + Wd + ' ' + Hd + '" width="100%" preserveAspectRatio="xMidYMid meet" role="img" aria-label="1日の詳細タスク別レーン">'];
    for (var h = 0; h <= 24; h += 3) {
      var x = xd(h * 60);
      svg.push('<line class="grid-line" x1="' + x + '" y1="' + topH + '" x2="' + x + '" y2="' + (Hd - 4) + '" opacity="0.35"/>');
      svg.push('<text class="axis-lab" x="' + x + '" y="11" text-anchor="middle">' + h + '</text>');
    }
    // 並列度ストリップ
    var sy0 = topH, sy1 = topH + stripH;
    svg.push('<rect x="' + labW2 + '" y="' + sy0 + '" width="' + (Wd - labW2 - 8) + '" height="' + stripH + '" fill="var(--card)" fill-opacity="0.4" rx="4"/>');
    svg.push('<text class="axis-lab" x="' + (labW2 - 8) + '" y="' + (sy0 + 12) + '" text-anchor="end">並列度</text>');
    svg.push('<text class="axis-lab" x="' + (labW2 - 8) + '" y="' + (sy1 - 3) + '" text-anchor="end">' + maxC + '</text>');
    var pts = [labW2 + "," + sy1];
    for (var mn = 0; mn <= 1440; mn += 15) { pts.push(xd(mn) + "," + (sy1 - (concAt(day, mn) / maxC) * (stripH - 4))); }
    pts.push((Wd - 8) + "," + sy1);
    svg.push('<polygon points="' + pts.join(" ") + '" fill="var(--accent)" fill-opacity="0.28" stroke="var(--accent)" stroke-opacity="0.6" stroke-width="1"/>');
    // タスク別レーン
    all.forEach(function (tk, i) {
      var y = topH + stripH + 8 + i * (laneH + laneGap);
      svg.push('<rect x="' + labW2 + '" y="' + y + '" width="' + (Wd - labW2 - 8) + '" height="' + laneH + '" rx="4" fill="var(--card)" fill-opacity="0.45"/>');
      svg.push('<rect x="0" y="' + (y + laneH / 2 - 5) + '" width="10" height="10" rx="2" fill="' + colorOf(tk.proj) + '"/>');
      svg.push('<text class="lane-id" x="16" y="' + (y + laneH / 2 + 3) + '">#' + esc(tk.id) + '</text>');
      svg.push('<text class="lane-ti" x="58" y="' + (y + laneH / 2 + 3) + '">' + esc(clip(tk.ti, 16)) + '</text>');
      svg.push('<text class="lane-dr" x="' + (labW2 - 8) + '" y="' + (y + laneH / 2 + 3) + '" text-anchor="end">' + hm(tk.total) + '</text>');
      tk.ivs.forEach(function (iv) {
        var x1 = xd(iv.s), x2 = xd(iv.e), w = Math.max(2, x2 - x1);
        var tip = "#" + esc(tk.id) + " " + esc(tk.ti) + " " + fmtT(iv.s) + "–" + fmtT(iv.e);
        svg.push('<rect x="' + x1.toFixed(1) + '" y="' + (y + 4) + '" width="' + w.toFixed(1) + '" height="' + (laneH - 8) + '" rx="2.5" fill="' + colorOf(tk.proj) + '" fill-opacity="0.9" data-tip="' + tip + '" aria-label="' + tip + '"/>');
      });
    });
    svg.push('</svg>');
    var foldNote = folded > 0 ? '<div class="d-fold">＋ 他 ' + folded + ' タスク (合計 ' + hm(foldedSec) + ') を省略 (稼働の多い上位 ' + MAX_LANE + ' 件を表示)</div>' : '';
    var box = document.getElementById("detail");
    box.innerHTML = head + svg.join("") + foldNote;
    document.getElementById("dBack").addEventListener("click", function () {
      document.getElementById("lanes").scrollIntoView({ behavior: "smooth", block: "center" });
    });
  }

  function pcGuard() {
    document.getElementById("pcwarn").style.display = window.innerWidth < 820 ? "block" : "none";
  }
  window.addEventListener("resize", pcGuard);

  // カスタムツールチップ。ネイティブ SVG <title> はブラウザ UI が描画し、ページの font-family も
  // lang="ja" も継承しないため、環境によっては日本語が中国語グリフで出る (0139)。data-tip を持つ
  // 要素にページ内 DOM のツールチップ (--sans + lang=ja が効く) を出して日本語グリフを保証する。
  // render() で innerHTML を作り直しても効くよう、document への委譲で 1 度だけ張る。
  function initTooltip() {
    var tip = document.createElement("div");
    tip.className = "tooltip";
    tip.style.display = "none";
    document.body.appendChild(tip);
    var cur = null;
    function place(e) {
      var pad = 14, tw = tip.offsetWidth, th = tip.offsetHeight;
      var x = e.clientX + pad, y = e.clientY + pad;
      if (x + tw > window.innerWidth - 4) x = Math.max(4, e.clientX - tw - pad);
      if (y + th > window.innerHeight - 4) y = Math.max(4, e.clientY - th - pad);
      tip.style.left = x + "px"; tip.style.top = y + "px";
    }
    function hide() { if (cur) { cur = null; tip.style.display = "none"; } }
    function tipTarget(e) { var t = e.target; return t && t.closest ? t.closest("[data-tip]") : null; }
    document.addEventListener("mouseover", function (e) {
      var el = tipTarget(e); if (!el) return;
      cur = el; tip.textContent = el.getAttribute("data-tip"); tip.style.display = "block"; place(e);
    });
    document.addEventListener("mousemove", function (e) {
      if (!cur) return;
      if (!cur.isConnected) { hide(); return; } // 再描画で対象が消えたときの保険
      place(e);
    });
    document.addEventListener("mouseout", function (e) {
      if (cur && tipTarget(e) === cur) hide();
    });
  }
  initTooltip();

  // このビューは自動更新しない (0134)。ロード時に一度だけ描画する。最新データが要るときは手動リロード。
  render();
}

// ブラウザ: bootstrap script が注入した window.PIECES / window.COLORS で起動する。
if (typeof window !== "undefined" && typeof document !== "undefined" && window.PIECES) {
  startParallel(window.PIECES, window.COLORS);
}

// Node (vitest): 純粋関数だけを公開する (描画コードは上のガードで走らない)。
// このファイルは <script> インライン展開されるため ESM export は使えず、CommonJS の存在ガードで公開する。
if (typeof module !== "undefined" && module.exports) {
  module.exports = { WD, esc, clip, hm, fmtT, pad2, parseD, dowOf, dlabel, concAt, datesDesc, byDate, mostActive };
}
