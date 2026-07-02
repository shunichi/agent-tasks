package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"
)

// バージョン表示。main に継続的にマージするスタイル (タグ運用なし) に合わせ、
// 手動 bump はせず **ビルド時の VCS 情報**を実行時に出す。Go は `go build` 時に
// commit / 日時 / dirty をバイナリへ自動埋め込みするので (runtime/debug.ReadBuildInfo)、
// ldflags も VERSION 定数も要らない。人間可読のため commit 日時から CalVer を併記する。
//
// CHANGELOG (main マージ日の日付セクション) は「いつ何が変わったか」、version は「どの commit
// 時点のビルドか」を示す補完関係。skill も同じ repo から出るので version は CLI/skill 一式の版。

// progName はこのバイナリの名前 (表示・state dir・補完のコマンド名の基点)。
// 既定は "agent-tasks"。稼働中の別ビルドと共存させるため、ビルド時に
// `-ldflags "-X main.progName=agent-tasks-herdr"` で上書きできる (var にしてある理由)。
// これにより state dir (session.go) と補完 (completion.go) が別名側で自動的に分離される。
// 注意: ストア (store.go の storeDir) は progName 由来にせず共有する (両ビルドで同じ
// ~/agent-tasks-store を読み書きし、データ互換を保つため)。
var progName = "agent-tasks"

// shortSHALen は表示する commit SHA の桁数 (衝突しにくさと読みやすさのバランス)。
const shortSHALen = 12

// vcsInfo はビルドに埋め込まれた VCS 情報。ReadBuildInfo の Settings から拾う。
type vcsInfo struct {
	revision string // commit SHA (フル)。VCS 情報が無ければ ""
	time     string // commit 日時 (RFC3339)
	modified bool   // ビルド時に作業ツリーが dirty だったか
}

// readVCSInfo は runtime/debug.ReadBuildInfo() から VCS 情報を取り出す。
// `go build -buildvcs=false` や VCS 外ビルドでは revision が "" になる。
func readVCSInfo() vcsInfo {
	var v vcsInfo
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			v.revision = s.Value
		case "vcs.time":
			v.time = s.Value
		case "vcs.modified":
			v.modified = s.Value == "true"
		}
	}
	return v
}

// formatVersion は VCS 情報を 1 行 + 詳細の文字列に整形する (純関数: テスト用)。
// 例: "agent-tasks 2026.06.29+g904ff2b1c2d3 (built from 904ff2b1c2d3, 2026-06-29T16:46:24+09:00, clean)"
// VCS 情報が無いときは "agent-tasks (devel)"。
func formatVersion(v vcsInfo) string {
	if v.revision == "" {
		return progName + " (devel)"
	}
	short := v.revision
	if len(short) > shortSHALen {
		short = short[:shortSHALen]
	}
	calver := "0.0.0" // commit 日時が読めないときのフォールバック
	if t, err := time.Parse(time.RFC3339, v.time); err == nil {
		calver = t.Format("2006.01.02")
	}
	state := "clean"
	if v.modified {
		state = "dirty"
	}
	detail := "built from " + short
	if v.time != "" {
		detail += ", " + v.time
	}
	detail += ", " + state
	return fmt.Sprintf("%s %s+g%s (%s)", progName, calver, short, detail)
}

// cmdVersion は version サブコマンド (と --version / -V)。
func cmdVersion(args []string) error {
	for _, a := range args {
		return usagef("version: unexpected argument %q", a)
	}
	fmt.Fprintln(os.Stdout, formatVersion(readVCSInfo()))
	return nil
}
