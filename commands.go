package main

import (
	"fmt"
	"os"
)

// command はサブコマンド 1 つの定義。これまで別々に手で維持していて「食い違うと無言で
// 壊れる」次の重複を 1 つの表に集約する:
//
//   - dispatch の振り分け (旧 main.go の巨大 switch)。
//   - 補完のサブコマンド一覧と説明 (completion.go の completionSubcommands)。
//   - bash 補完の per-sub フラグ一覧 (bashCompletionScript が本表から生成する)。
//
// zsh 補完 (_arguments) はフラグ説明や値補完器 (project/id を動的に列挙する) を持ち、
// それらは本表に載らない情報なので、本表からは機械生成せず completion.go に手書きで残す。
// 代わりに commands_test.go が「本表のフラグが bash 生成に載る」「visible コマンドが
// usage() に載る」ことを検査し、追加時の付け忘れ (ドリフト) を CI で検出する。
type command struct {
	name   string
	desc   string               // 補完のサブコマンド説明 (zsh 用)。hidden では空でよい
	run    func([]string) error // dispatch 先
	flags  []string             // コマンド固有フラグ (--color/--help は補完生成時に自動付与)
	hidden bool                 // 内部コマンド: dispatch はするが補完/ヘルプには出さない
}

// listFlags は list (= サブコマンド無しの既定) が受けるフラグ。補完の top-level (サブコマンド
// 未確定時) と list サブコマンドで共有する (両者は同じ集合)。
var listFlags = []string{
	"--all-projects", "--all", "-a", "--status", "--kind", "--project", "--projects",
	"--search", "--grep", "--content", "--full", "--watch", "-w", "--interval",
	"--active", "--recent", "--archived", "--json",
}

// commands は全サブコマンドの定義 (visible は補完に出す順で並べる)。
//
// init() で組み立てるのは初期化サイクル回避のため。commands の要素は run に cmdCompletion を
// 持ち、cmdCompletion は補完生成で completionSubcommands()→commands を読む。これを package 変数の
// 初期化子で書くと commands ↔ commands のサイクルとしてコンパイルエラーになる。init() 本体は
// 変数初期化サイクル解析の対象外なので、ここで代入すると解決する。
var commands []command

// commandByName は commands を名前で引くための索引 (dispatch 用)。init() で commands から作る。
var commandByName map[string]*command

func init() {
	commands = []command{
		{name: "list", desc: "現在 project のタスク一覧 (既定)", run: cmdList, flags: listFlags},
		{name: "tui", desc: "一覧+詳細をインタラクティブに閲覧 (自動更新)", run: cmdTUI,
			flags: []string{"--status", "--project", "--projects", "--all-projects", "--all", "--interval"}},
		{name: "serve", desc: "同一 LAN のスマホから閲覧する簡易 HTTP サーバ", run: cmdServe,
			flags: []string{"--addr", "--interval", "--project", "--projects", "--all-projects", "--status", "--kind", "--all"}},
		{name: "show", desc: "1 タスクの全文を表示", run: cmdShow,
			flags: []string{"--archived", "--json"}},
		{name: "edit", desc: "ストア/タスクをエディタで開く", run: cmdEdit},
		{name: "open", desc: "タスクの worktree をエディタで開く", run: cmdOpen},
		{name: "focus", desc: "実行中タスクの herdr pane にフォーカスを移す (herdr)", run: cmdFocus},
		{name: "status", desc: "ストアの未同期状態を表示", run: cmdStatus},
		{name: "sync", desc: "ストアを add/commit/push して同期", run: cmdSync,
			flags: []string{"--no-push", "--push"}},
		{name: "worktree-init", desc: "worktree 作成後フックを実行", run: cmdWorktreeInit,
			flags: []string{"--force"}},
		{name: "worktree-remove", desc: "worktree 撤去フック + git worktree remove", run: cmdWorktreeRemove,
			flags: []string{"--force", "--hook-only"}},
		{name: "scaffold-worktree", desc: "worktree 設定の雛形を展開", run: cmdScaffoldWorktree,
			flags: []string{"--list", "--dir", "--force"}},
		{name: "doctor", desc: "id 重複/不一致を点検", run: cmdDoctor,
			flags: []string{"--project"}},
		{name: "archive", desc: "タスクを退避 (削除せず一覧から外す)", run: cmdArchive},
		{name: "auto-archive", desc: "完了後に一定期間経過した done を一括退避", run: cmdAutoArchive,
			flags: []string{"--older-than", "--project", "--projects", "--all-projects", "--dry-run"}},
		{name: "unarchive", desc: "退避したタスクを元に戻す", run: cmdUnarchive},
		{name: "issue", desc: "タスクを GitHub issue として共有", run: cmdIssue,
			flags: []string{"--repo"}},
		{name: "cost", desc: "タスクの Claude トークン消費/概算コストを集計", run: cmdCost,
			flags: []string{"--json", "--record"}},
		{name: "report", desc: "一定期間の完了タスクを markdown で出力", run: cmdReport,
			flags: []string{"--month", "--week", "--since", "--until", "--project", "--projects", "--all-projects"}},
		{name: "worktime", desc: "タスクの実稼働時間 (working 合計) と稼働区間を表示", run: cmdWorktime,
			flags: []string{"--all", "--json", "--project", "--projects", "--all-projects"}},
		{name: "session-hook", desc: "Claude Code の hook から呼ぶ", run: cmdSessionHook,
			flags: []string{"--print-config"}},
		{name: "session-link", desc: "セッションをタスクに紐づける", run: cmdSessionLink,
			flags: []string{"--session", "--project"}},
		{name: "session-rename", desc: "現在の Claude セッション名をタスク名に変える (tmux)", run: cmdSessionRename,
			flags: []string{"--project"}},
		{name: "session-prune", desc: "state dir の古いマーカー/link を掃除する", run: cmdSessionPrune,
			flags: []string{"--older-than", "--dry-run"}},
		{name: "statusline", desc: "実行中タスクを status line に表示", run: cmdStatusline,
			flags: []string{"--print-config"}},
		{name: "alloc-id", desc: "タスク id を原子的に採番し予約ファイルを作成", run: cmdAllocID,
			flags: []string{"--slug", "--title", "--kind", "--body-file", "--project", "--pull"}},
		{name: "spawn", desc: "別 pane で新セッションを開き対象タスクに着手させる (herdr)", run: cmdSpawn,
			flags: []string{"--split", "--focus", "--force"}},
		{name: "claim", desc: "着手時に in-progress をロック下で原子的に予約 (start の TOCTOU 回避)", run: cmdClaim,
			flags: []string{"--agent", "--session", "--release", "--to", "--force"}},
		{name: "resume", desc: "blocked/review を in-progress に戻して作業を再開", run: cmdResume,
			flags: []string{"--agent", "--session"}},
		{name: "done", desc: "完了/レビュー待ちの frontmatter を確定 (status/completed_at)", run: cmdDone,
			flags: []string{"--review"}},
		{name: "block", desc: "保留の frontmatter を確定 (status/blocked_at/blocked_reason)", run: cmdBlock,
			flags: []string{"--reason"}},
		{name: "where", desc: "データディレクトリのパスを表示", run: cmdWhere},
		{name: "version", desc: "ビルド元の commit + CalVer を表示", run: cmdVersion},
		{name: "completion", desc: "シェル補完スクリプトを出力", run: cmdCompletion},
		{name: "help", desc: "ヘルプを表示", run: cmdHelp},

		// 内部コマンド (補完/ヘルプに出さない。dispatch はする)。
		{name: "completion-values", run: cmdCompletionValues, hidden: true},
		{name: "worktime-record", run: cmdWorktimeRecord, hidden: true},
		{name: "tui-overlay", run: cmdTuiOverlay, hidden: true},
		{name: "herdr-probe", run: cmdHerdrProbe, hidden: true},
	}

	commandByName = make(map[string]*command, len(commands))
	for i := range commands {
		commandByName[commands[i].name] = &commands[i]
	}
}

// cmdWhere はデータディレクトリのパスを表示する (旧 dispatch の where インライン処理を関数化)。
func cmdWhere(args []string) error {
	fmt.Println(storeDir())
	return nil
}

// cmdHelp はヘルプを表示する。dispatch では help/-h/--help を先に特別扱いするため通常は
// ここへ到達しないが、レジストリの run を揃えておく (completion 一覧に help を出すため)。
func cmdHelp(args []string) error {
	usage(os.Stdout)
	return nil
}
