package main

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// completion サブコマンドはシェル補完スクリプトを stdout に出力する。
// 外部の補完フレームワークは使わず、bash / zsh それぞれの
// スクリプトを自前で組み立てる。補完候補の一次情報 (サブコマンド名・列挙可能な
// フラグ値) はここに集約し、両シェルのスクリプトを同じデータから生成する。
//
// 静的補完 (サブコマンド名 + 列挙できるフラグ値) に加え、ストアの状態を見て候補を出す
// 動的補完 (--project の値・id 引数) を持つ。動的候補は生成スクリプトから
// `agent-tasks completion-values <kind>` を呼び、1 行 1 候補のプレーン出力を列挙させる
// (補完専用の内部コマンド。失敗しても補完が壊れないよう空で返す)。

// completionSubcommand は補完で提示するサブコマンドとその説明 (zsh 用)。
type completionSubcommand struct{ name, desc string }

var completionSubcommands = []completionSubcommand{
	{"list", "現在 project のタスク一覧 (既定)"},
	{"tui", "一覧+詳細をインタラクティブに閲覧 (自動更新)"},
	{"show", "1 タスクの全文を表示"},
	{"edit", "ストア/タスクをエディタで開く"},
	{"open", "タスクの worktree をエディタで開く"},
	{"status", "ストアの未同期状態を表示"},
	{"sync", "ストアを add/commit/push して同期"},
	{"worktree-init", "worktree 作成後フックを実行"},
	{"worktree-remove", "worktree 撤去フック + git worktree remove"},
	{"scaffold-worktree", "worktree 設定の雛形を展開"},
	{"doctor", "id 重複/不一致を点検"},
	{"archive", "タスクを退避 (削除せず一覧から外す)"},
	{"auto-archive", "完了後に一定期間経過した done を一括退避"},
	{"unarchive", "退避したタスクを元に戻す"},
	{"issue", "タスクを GitHub issue として共有"},
	{"report", "一定期間の完了タスクを markdown で出力"},
	{"session-hook", "Claude Code の hook から呼ぶ"},
	{"session-link", "セッションをタスクに紐づける"},
	{"session-rename", "現在の Claude セッション名をタスク名に変える (tmux)"},
	{"statusline", "実行中タスクを status line に表示"},
	{"alloc-id", "タスク id を原子的に採番し予約ファイルを作成"},
	{"where", "データディレクトリのパスを表示"},
	{"version", "ビルド元の commit + CalVer を表示"},
	{"completion", "シェル補完スクリプトを出力"},
	{"help", "ヘルプを表示"},
}

// 列挙できるフラグ値 (静的補完で候補を出せるもの)。
var (
	completionStatusValues = []string{"todo", "in-progress", "blocked", "review", "done"}
	completionKindValues   = []string{"human", "code"}
	completionColorValues  = []string{"always", "auto", "never"}
	completionShellValues  = []string{"bash", "zsh"}
)

func subcommandNames() []string {
	names := make([]string, len(completionSubcommands))
	for i, s := range completionSubcommands {
		names[i] = s.name
	}
	return names
}

func cmdCompletion(args []string) error {
	if len(args) == 0 {
		return usagef("completion requires a shell (bash|zsh)")
	}
	shell := args[0]
	rest := args[1:]
	for _, a := range rest {
		return usagef("completion: unexpected argument %q", a)
	}
	switch shell {
	case "bash":
		fmt.Print(bashCompletionScript())
	case "zsh":
		fmt.Print(zshCompletionScript())
	default:
		return usagef("completion: unknown shell %q (want bash|zsh)", shell)
	}
	_ = os.Stdout.Sync()
	return nil
}

// cmdCompletionValues は補完スクリプトが動的候補を得るための内部コマンド。
// 1 行 1 候補のプレーン出力を stdout に列挙する (色やヘッダは付けない)。
//   - completion-values projects        ストア配下の project (ディレクトリ名)
//   - completion-values ids [--project <name>]  project のタスク id (既定: 現在 project)
//
// 補完を壊さないため、ストアが無い/読めない等は**エラーにせず空出力**で返す。
func cmdCompletionValues(args []string) error {
	if len(args) == 0 {
		return usagef("completion-values requires a kind (projects|ids)")
	}
	kind, rest := args[0], args[1:]
	switch kind {
	case "projects":
		for _, a := range rest {
			return usagef("completion-values projects: unexpected argument %q", a)
		}
		printProjects(os.Stdout)
		return nil
	case "ids":
		project := ""
		withTitle := false
		archived := false
		s := newArgScan(rest)
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
				project = v
			case "--with-title":
				// 各 id にタブ区切りでタイトルを添える (zsh の説明付き補完用)。
				withTitle = true
			case "--archived":
				// アクティブではなくアーカイブ済み (<project>/archive/) の id を列挙 (unarchive 補完用)。
				archived = true
			default:
				return usagef("completion-values ids: unexpected argument %q", a)
			}
		}
		if pos := s.rest(); len(pos) > 0 {
			return usagef("completion-values ids: unexpected argument %q", pos[0])
		}
		if project == "" {
			project = currentProject()
		}
		if project != "" {
			dir := filepath.Join(storeDir(), project)
			if archived {
				dir = filepath.Join(dir, archiveDirName)
			}
			if withTitle {
				printTaskIDsWithTitle(os.Stdout, dir)
			} else {
				printTaskIDs(os.Stdout, dir)
			}
		}
		return nil
	default:
		return usagef("completion-values: unknown kind %q (want projects|ids)", kind)
	}
}

// printProjects はストア配下のディレクトリ名 (project キー) を昇順で1行ずつ出力する。
// 走査に失敗したら静かに何も出さない (補完を壊さない)。
func printProjects(w io.Writer) {
	entries, err := os.ReadDir(storeDir())
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		// 隠しディレクトリ (.git など) は project ではないので除く。
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for _, n := range names {
		fmt.Fprintln(w, n)
	}
}

// printTaskIDs は dir 直下のタスク id を昇順で1行ずつ出力する。
// id はファイル名先頭の連番 (<NNNN>-*.md) から取る (frontmatter を読まず高速)。
// dir が無い/読めないときは静かに何も出さない (アクティブなら <project>/、アーカイブなら
// <project>/archive/ を呼び出し側が渡す)。
func printTaskIDs(w io.Writer, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if id := leadingID(e.Name()); id != "" {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		fmt.Fprintln(w, id)
	}
}

// printTaskIDsWithTitle は dir 直下のタスクを id 昇順で "<id>\t<title>" 形式で出力する。
// タイトルを得るため frontmatter を読む (printTaskIDs より重いが、zsh の説明付き補完用)。
// 読めないファイルは飛ばし、dir が無い/読めないときは静かに何も出さない。
func printTaskIDsWithTitle(w io.Writer, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type row struct{ id, title string }
	var rows []row
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		t, err := parseTask(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		id := cmp.Or(t.ID, leadingID(e.Name()))
		if id == "" {
			continue
		}
		rows = append(rows, row{id, t.Title})
	}
	slices.SortFunc(rows, func(a, b row) int { return cmp.Compare(a.id, b.id) })
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\n", r.id, r.title)
	}
}

func bashCompletionScript() string {
	subs := strings.Join(subcommandNames(), " ")
	statuses := strings.Join(completionStatusValues, " ")
	kinds := strings.Join(completionKindValues, " ")
	colors := strings.Join(completionColorValues, " ")
	shells := strings.Join(completionShellValues, " ")
	topFlags := "--all-projects --all -a --status --kind --project --projects --search --grep --content --full --watch -w --interval --active --recent --archived --json --color --help"

	return fmt.Sprintf(`# bash completion for agent-tasks
# 有効化: source <(agent-tasks completion bash)
# 恒久化: agent-tasks completion bash > ~/.local/share/bash-completion/completions/agent-tasks
_agent_tasks() {
    local cur prev cword
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    cword=$COMP_CWORD

    # 値を取るフラグの直後は、その値の候補を出す。
    case "$prev" in
        --status)   COMPREPLY=( $(compgen -W "%[1]s" -- "$cur") ); return ;;
        --kind)     COMPREPLY=( $(compgen -W "%[6]s" -- "$cur") ); return ;;
        --color)    COMPREPLY=( $(compgen -W "%[2]s" -- "$cur") ); return ;;
        --project|--projects)  COMPREPLY=( $(compgen -W "$(agent-tasks completion-values projects 2>/dev/null)" -- "$cur") ); return ;;
        completion) COMPREPLY=( $(compgen -W "%[3]s" -- "$cur") ); return ;;
    esac

    # プログラム名の次にある最初の非フラグ語をサブコマンドとみなす。
    local sub="" i
    for (( i=1; i < cword; i++ )); do
        case "${COMP_WORDS[i]}" in
            -*) ;;
            *) sub="${COMP_WORDS[i]}"; break ;;
        esac
    done

    if [[ -z "$sub" ]]; then
        if [[ "$cur" == -* ]]; then
            COMPREPLY=( $(compgen -W "%[4]s" -- "$cur") )
        else
            COMPREPLY=( $(compgen -W "%[5]s" -- "$cur") )
        fi
        return
    fi

    # 位置引数 ([<project>] <id>) を取るサブコマンドの補完。
    #   第1引数: project 名 または現在 project の id
    #   第2引数: 第1引数を project とみなしてその id
    # 値を取るフラグ (--session は自由入力) の直後は除く。
    case "$sub" in
        show|edit|open|session-link|session-rename|archive|unarchive|issue)
            if [[ "$cur" != -* && "$prev" != "--session" && "$prev" != "--repo" ]]; then
                # unarchive はアーカイブ済みの id を補完する (それ以外はアクティブ)。
                local idopt=""
                [[ "$sub" == "unarchive" ]] && idopt="--archived"
                # サブコマンド後の位置引数を数える (フラグとその値を除く)。
                local -a pos=()
                local j skip=0
                for (( j=1; j < cword; j++ )); do
                    local w="${COMP_WORDS[j]}"
                    if (( skip )); then skip=0; continue; fi
                    case "$w" in
                        "$sub")                              ;;  # サブコマンド自身
                        --project|--session|--color|--repo)  skip=1 ;;  # フラグ値をスキップ
                        -*)                                  ;;
                        *)                            pos+=("$w") ;;
                    esac
                done
                if (( ${#pos[@]} == 0 )); then
                    COMPREPLY=( $(compgen -W "$(agent-tasks completion-values projects 2>/dev/null) $(agent-tasks completion-values ids $idopt 2>/dev/null)" -- "$cur") )
                else
                    COMPREPLY=( $(compgen -W "$(agent-tasks completion-values ids --project "${pos[0]}" $idopt 2>/dev/null)" -- "$cur") )
                fi
                return
            fi
            ;;
    esac

    local flags="--color --help"
    case "$sub" in
        list)              flags="%[4]s" ;;
        tui)               flags="--status --project --projects --all-projects --all --interval --color --help" ;;
        report)            flags="--month --week --since --until --project --projects --all-projects --color --help" ;;
        show)              flags="--archived --json --color --help" ;;
        edit)              flags="--color --help" ;;
        archive|unarchive) flags="--color --help" ;;
        auto-archive)      flags="--older-than --project --projects --all-projects --dry-run --color --help" ;;
        issue)             flags="--repo --color --help" ;;
        doctor)            flags="--project --color --help" ;;
        sync)              flags="--no-push --push --color --help" ;;
        scaffold-worktree) flags="--list --dir --force --color --help" ;;
        worktree-init)     flags="--force --color --help" ;;
        worktree-remove)   flags="--force --hook-only --color --help" ;;
        session-hook)      flags="--print-config --color --help" ;;
        session-link)      flags="--session --project --color --help" ;;
        session-rename)    flags="--project --color --help" ;;
        statusline)        flags="--print-config --color --help" ;;
        alloc-id)          flags="--slug --project --pull --color --help" ;;
        completion)        COMPREPLY=( $(compgen -W "%[3]s" -- "$cur") ); return ;;
    esac
    if [[ "$cur" == -* ]]; then
        COMPREPLY=( $(compgen -W "$flags" -- "$cur") )
    fi
}
complete -F _agent_tasks agent-tasks
`, statuses, colors, shells, topFlags, subs, kinds)
}

func zshCompletionScript() string {
	var subLines strings.Builder
	for _, s := range completionSubcommands {
		// 説明中の特殊文字を避けるため、説明はそのまま (記号を含まない前提)。
		fmt.Fprintf(&subLines, "        '%s:%s'\n", s.name, s.desc)
	}
	statuses := strings.Join(completionStatusValues, " ")
	colors := strings.Join(completionColorValues, " ")

	return fmt.Sprintf(`#compdef agent-tasks
# zsh completion for agent-tasks
# 有効化: fpath の通ったディレクトリに置いて再ログイン
#   agent-tasks completion zsh > ~/.local/share/zsh/site-functions/_agent_tasks
#   (そのディレクトリを compinit より前に fpath へ追加しておく)

# 動的補完のヘルパ: agent-tasks を呼んで候補を列挙する (失敗時は空 = 補完を壊さない)。
(( $+functions[_agent_tasks_projects] )) || _agent_tasks_projects() {
    local -a ps
    ps=(${(f)"$(agent-tasks completion-values projects 2>/dev/null)"})
    compadd -a ps
}
# _agent_tasks_ids [<project>] [<archived>]: project (省略時は現在 project) のタスク id を、
# タイトルを説明に添えて補完する ("0001  タイトル" と表示し、挿入されるのは id のみ)。
# 第2引数が非空ならアーカイブ済みの id を出す (unarchive 補完用)。
(( $+functions[_agent_tasks_ids] )) || _agent_tasks_ids() {
    local proj=$1
    local arch=$2
    local -a lines ids descs
    lines=(${(f)"$(agent-tasks completion-values ids ${proj:+--project $proj} ${arch:+--archived} --with-title 2>/dev/null)"})
    local l
    for l in $lines; do
        ids+=${l%%$'\t'*}
        descs+="${l%%$'\t'*}  ${l#*$'\t'}"
    done
    (( ${#ids} )) && compadd -d descs -- $ids
}

_agent_tasks() {
    local -a subcommands
    subcommands=(
%[1]s    )

    # 値を取る大域フラグの直後は、サブコマンドの有無に関わらず値を補完する
    # (例: サブコマンド無しの "agent-tasks --project <TAB>")。bash 版の $prev 処理に対応。
    case ${words[CURRENT-1]} in
        --project|--projects) _agent_tasks_projects; return ;;
        --status)  compadd %[2]s; return ;;
        --kind)    compadd %[5]s; return ;;
        --color)   compadd %[3]s; return ;;
    esac

    # プログラム名の次にある最初の非フラグ語をサブコマンドとみなす。
    local sub="" i
    for (( i = 2; i < CURRENT; i++ )); do
        if [[ ${words[i]} != -* ]]; then
            sub=${words[i]}
            break
        fi
    done

    if [[ -z $sub ]]; then
        if [[ ${words[CURRENT]} == -* ]]; then
            _values 'option' \
                '--all-projects[全 project を横断]' \
                '--all[done も含める]' '-a[done も含める]' \
                '--status[status で絞り込み]' \
                '--kind[種別で絞り込み (human/code)]' \
                '--project[project を指定 (繰り返し可)]' \
                '--projects[project をカンマ区切りで複数指定]' \
                '--search[タイトル検索]' '--grep[タイトル検索]' \
                '--content[本文も検索]' '--full[本文も検索]' \
                '--watch[自動更新]' '-w[自動更新]' \
                '--interval[更新間隔(秒)]' \
                '--active[着手中のみ]' \
                '--recent[最近完了 N 件]' \
                '--archived[アーカイブ済みのみ]' \
                '--json[JSON 出力]' \
                '--color[色出力]' '--help[ヘルプ]'
        else
            _describe -t commands 'agent-tasks command' subcommands
        fi
        return
    fi

    case $sub in
        list)
            _arguments \
                '--all-projects[全 project を横断]' \
                '(--all -a)'{--all,-a}'[done も含める]' \
                '--status[status で絞り込み]:status:(%[2]s)' \
                '--kind[種別で絞り込み]:kind:(%[5]s)' \
                '*--project[project を指定 (繰り返し可)]:project:_agent_tasks_projects' \
                '*--projects[project をカンマ区切りで複数指定]:projects:_agent_tasks_projects' \
                '(--search --grep)'{--search,--grep}'[タイトル検索]:query:' \
                '(--content --full)'{--content,--full}'[本文も検索対象にする]' \
                '(--watch -w)'{--watch,-w}'[自動更新]' \
                '--interval[更新間隔(秒)]:seconds:' \
                '--active[着手中のみ]' \
                '--recent[最近完了 N 件]:count:' \
                '--archived[アーカイブ済みのみ]' \
                '--json[JSON 出力]' \
                '--color[色出力]:mode:(%[3]s)'
            ;;
        tui)
            _arguments \
                '--all-projects[全 project を横断]' \
                '(--all -a)'{--all,-a}'[done も含める]' \
                '--status[status で絞り込み]:status:(%[2]s)' \
                '*--project[project を指定 (繰り返し可)]:project:_agent_tasks_projects' \
                '*--projects[project をカンマ区切りで複数指定]:projects:_agent_tasks_projects' \
                '--interval[更新間隔(秒)]:seconds:' \
                '--color[色出力]:mode:(%[3]s)'
            ;;
        report)
            _arguments \
                '--month[月 (YYYY-MM、既定は今月)]:month:' \
                '--week[週 (YYYY-MM-DD を含む週)]:date:' \
                '--since[開始日 (YYYY-MM-DD)]:date:' \
                '--until[終了日 (YYYY-MM-DD、その日を含む)]:date:' \
                '*--project[project を指定 (繰り返し可)]:project:_agent_tasks_projects' \
                '*--projects[project をカンマ区切りで複数指定]:projects:_agent_tasks_projects' \
                '--all-projects[全 project を横断]' \
                '--color[色出力]:mode:(%[3]s)'
            ;;
        show|edit|open|archive|unarchive|issue)
            # [<project>] <id> の位置引数を補完する。フラグ入力中はフラグ候補。
            if [[ ${words[CURRENT]} == -* ]]; then
                if [[ $sub == show ]]; then
                    _values 'option' '--archived[アーカイブ済みを開く]' '--json[JSON 出力]' '--color[色出力]' '--help[ヘルプ]'
                elif [[ $sub == issue ]]; then
                    _values 'option' '--repo[owner/repo を明示]' '--color[色出力]' '--help[ヘルプ]'
                else
                    _values 'option' '--color[色出力]' '--help[ヘルプ]'
                fi
            else
                # unarchive はアーカイブ済みの id を補完する (それ以外はアクティブ)。
                local arch=""
                [[ $sub == unarchive ]] && arch=1
                # サブコマンド以降の位置引数を集める (フラグと値を除く)。
                # C 言語形式の for (( )) は zsh の補完文脈で表示を壊すため foreach を使う。
                local -a pos; local w skip=0
                for w in ${words[3,CURRENT-1]}; do
                    if (( skip )); then skip=0; continue; fi
                    case $w in
                        --color|--repo) skip=1 ;;   # 値を取るフラグの値はスキップ
                        -*) ;;
                        *) pos+=$w ;;
                    esac
                done
                if (( ${#pos} == 0 )); then
                    _agent_tasks_projects        # 第1引数: project 名 …
                    _agent_tasks_ids "" $arch    # … または現在 project の id
                else
                    _agent_tasks_ids ${pos[1]} $arch  # 第2引数: pos[1] の project の id
                fi
            fi
            ;;
        auto-archive)
            _arguments \
                '--older-than[完了後この日数を過ぎた done を対象 (既定 30)]:days:' \
                '*--project[project を指定 (繰り返し可)]:project:_agent_tasks_projects' \
                '*--projects[project をカンマ区切りで複数指定]:projects:_agent_tasks_projects' \
                '--all-projects[全 project を横断]' \
                '--dry-run[対象一覧のみ表示 (移動しない)]' \
                '--color[色出力]:mode:(%[3]s)'
            ;;
        completion)
            _values 'shell' %[4]s
            ;;
        doctor)
            _arguments \
                '--project[project を指定]:project:_agent_tasks_projects' \
                '--color[色出力]:mode:(%[3]s)'
            ;;
        sync)
            _arguments \
                '--no-push[commit まで (push しない)]' \
                '--push[push も行う]' \
                '--color[色出力]:mode:(%[3]s)'
            ;;
        scaffold-worktree)
            _arguments \
                '--list[stack 一覧を表示]' \
                '--dir[出力先ディレクトリ]:dir:_files -/' \
                '--force[既存を上書き]' \
                '--color[色出力]:mode:(%[3]s)'
            ;;
        worktree-init)
            _arguments \
                '--force[再実行する]' \
                '--color[色出力]:mode:(%[3]s)'
            ;;
        worktree-remove)
            _arguments \
                '--force[未コミット/フック失敗を無視して撤去]' \
                '--hook-only[フックだけ実行し撤去しない]' \
                '--color[色出力]:mode:(%[3]s)'
            ;;
        session-hook|statusline)
            _arguments \
                '--print-config[設定例を出力]' \
                '--color[色出力]:mode:(%[3]s)'
            ;;
        session-link)
            # [<project>] <id> の位置引数 + フラグ (--session/--project)。
            if [[ ${words[CURRENT]} == -* ]]; then
                _values 'option' \
                    '--session[session_id を明示]' \
                    '--project[project を指定]' \
                    '--color[色出力]' '--help[ヘルプ]'
            else
                # C 言語形式の for (( )) は zsh の補完文脈で表示を壊すため foreach を使う。
                local -a pos; local w skip=0
                for w in ${words[3,CURRENT-1]}; do
                    if (( skip )); then skip=0; continue; fi
                    case $w in
                        --session|--project|--color) skip=1 ;;
                        -*) ;;
                        *) pos+=$w ;;
                    esac
                done
                if (( ${#pos} == 0 )); then
                    _agent_tasks_projects
                    _agent_tasks_ids
                else
                    _agent_tasks_ids ${pos[1]}
                fi
            fi
            ;;
        session-rename)
            # [<project>] <id> の位置引数 + フラグ (--project)。
            if [[ ${words[CURRENT]} == -* ]]; then
                _values 'option' \
                    '--project[project を指定]' \
                    '--color[色出力]' '--help[ヘルプ]'
            else
                local -a pos; local w skip=0
                for w in ${words[3,CURRENT-1]}; do
                    if (( skip )); then skip=0; continue; fi
                    case $w in
                        --project|--color) skip=1 ;;
                        -*) ;;
                        *) pos+=$w ;;
                    esac
                done
                if (( ${#pos} == 0 )); then
                    _agent_tasks_projects
                    _agent_tasks_ids
                else
                    _agent_tasks_ids ${pos[1]}
                fi
            fi
            ;;
        alloc-id)
            _arguments \
                '--slug[内容を表すケバブケース]:slug:' \
                '--project[project を指定]:project:_agent_tasks_projects' \
                '--pull[採番前にストアを pull --rebase]' \
                '--color[色出力]:mode:(%[3]s)'
            ;;
        *)
            _arguments \
                '--color[色出力]:mode:(%[3]s)' \
                '--help[ヘルプ]'
            ;;
    esac
}
_agent_tasks "$@"
`, subLines.String(), statuses, colors, shellsForValues(), strings.Join(completionKindValues, " "))
}

// shellsForValues は zsh の _values 用に "'bash' 'zsh'" のようにクォートして並べる。
func shellsForValues() string {
	quoted := make([]string, len(completionShellValues))
	for i, s := range completionShellValues {
		quoted[i] = "'" + s + "'"
	}
	return strings.Join(quoted, " ")
}
