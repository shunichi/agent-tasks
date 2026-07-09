package main

// serve の 3 ビュー (一覧 / 時間配分 / 時間帯・並列) 共通のビュー切替タブ。
//
// 以前は各ビューの <h1> に個別のリンクが場当たり的に埋まっており、(1) 一覧から時間帯・並列へ
// 直接行けない、(2) 同じビューの呼び名が「稼働時間 / 時間配分」で不統一、(3) 現在地の明示が無い、
// (4) 矢印の向きが不整合、という分かりづらさがあった (0136)。
//
// そこで名前・並び順・URL・スタイルをここ 1 箇所に集約したセグメント型タブに統一する。各ビューの
// テンプレート文字列へ viewNavTmpl を連結し、ヘッダ内で {{template "viewnav" "<key>"}} を呼ぶと、
// key に対応するタブだけをアクセント色でアクティブ表示する。key: "list" / "alloc" / "parallel"。
//
// スタイルも同じ define 内の <style> に持たせる (HTML body に <style> を置くのは HTML5 で有効)。
// これで各ビューの <style> を編集せずに済み、CSS も含めて完全に単一ソースになる。CSS 内には
// テンプレートアクションが無いので html/template の CSS エスケープの影響も受けない。
const viewNavTmpl = `{{define "viewnav"}}` +
	`<style>` +
	`.viewnav{display:flex;gap:.3rem;margin-top:.5rem;flex-wrap:wrap}` +
	`.viewnav a{font-size:.8rem;text-decoration:none;color:var(--dim);background:var(--card);` +
	`border:1px solid var(--border);border-radius:6px;padding:.22rem .7rem;font-weight:500;line-height:1.4}` +
	`.viewnav a:hover{border-color:var(--accent);color:var(--accent)}` +
	`.viewnav a.on{background:var(--accent);color:#08111f;border-color:var(--accent);font-weight:600}` +
	`</style>` +
	`<nav class="viewnav">` +
	`<a href="/"{{if eq . "list"}} class="on" aria-current="page"{{end}}>一覧</a>` +
	`<a href="/worktime"{{if eq . "alloc"}} class="on" aria-current="page"{{end}}>時間配分</a>` +
	`<a href="/worktime?view=parallel"{{if eq . "parallel"}} class="on" aria-current="page"{{end}}>時間帯・並列</a>` +
	`</nav>{{end}}`
