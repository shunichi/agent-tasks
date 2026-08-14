package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// agentLastActivity は「そのセッションが最後に実作業をしていた時刻」を agent の transcript から
// 引く。worktime ログの working が閉じられなかったとき (worktime.go の sessionOpenEnd) に、
// 区間をどこで閉じるべきかの**証拠**として使う唯一の用途。
//
// なぜ transcript が証拠になるか: transcript は作業中ずっと書かれ続ける (実測で 53 分の working
// 区間に 598 エントリ = 約 5 秒に 1 件)。よって最終エントリの時刻は「作業が止まった時刻」の
// 良い近似で、閉じイベントを取りこぼした区間をここで打ち切れる。実測した未クローズ 24 本のうち
// 21 本は transcript が working イベント時刻と同時かそれ以前で終わっており、= その working は
// 実作業を伴わない空振り (入力キュー直後にプロセス死) だったと判定できる。
//
// ⚠️ ここがこのパッケージで唯一の **agent 依存**箇所。集計本体 (worktime.go) は agent 非依存を
// 保ち、agent ごとの transcript 置き場の違いはこの関数の中だけに閉じ込める。増やすときは
// transcriptLocators に 1 エントリ足す。見つからなければ ok=false を返し、呼び側は cap に
// フォールバックする (transcript の削除・別マシン記録・未知の agent はすべてこの経路)。

// transcriptCapBytes は transcript 末尾から読む最大バイト数。transcript は実測で最大 19.7MB /
// p90 3.3MB あるので全体は読まない。末尾から遡って最初に見つかった timestamp を採用する。
const transcriptCapBytes = 256 * 1024

// transcriptLocators は agent ごとの transcript 探索器。session_id を受け取り、見つかった
// ファイルパスを返す (無ければ "")。frontmatter の `agent:` では分岐せず**順に全部試す** —
// session_id は agent 間で衝突しないので単純に済み、agent 記録が無いタスクでも引ける。
var transcriptLocators = []func(sessionID string) string{
	claudeTranscriptPath,
	codexTranscriptPath,
}

// agentLastActivity は session_id の transcript 最終エントリの時刻を返す。
// transcript が無い / 時刻を持つ行が 1 つも無い場合は ok=false。
func agentLastActivity(sessionID string) (time.Time, bool) {
	if sessionID == "" {
		return time.Time{}, false
	}
	for _, locate := range transcriptLocators {
		path := locate(sessionID)
		if path == "" {
			continue
		}
		if ts, ok := lastTranscriptTimestamp(path); ok {
			return ts, true
		}
	}
	return time.Time{}, false
}

// claudeTranscriptPath は Claude Code の transcript (~/.claude/projects/<cwd 由来>/<sid>.jsonl)。
// project ディレクトリ名は cwd をエンコードしたもので、worktime 側からは cwd を復元できないため
// 全 project を舐める (未クローズ区間があるときだけ呼ばれるので、この走査は稀)。
func claudeTranscriptPath(sessionID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// codexTranscriptPath は codex の rollout ログ
// (~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<sid>.jsonl)。session_id が**ファイル名に入る**ので
// 日付ディレクトリを跨いで glob で引ける (どの日に始まったセッションかを知らなくてよい)。
func codexTranscriptPath(sessionID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(home, ".codex", "sessions", "*", "*", "*", "rollout-*-"+sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// lastTranscriptTimestamp は JSONL transcript の末尾から遡って最初に見つかった `timestamp` を返す。
// Claude Code / codex とも 1 行 1 JSON で `timestamp` (RFC3339) を持つ形式なので共通に扱える。
// 末尾には timestamp を持たない行 (メタ行) が混ざるため、見つかるまで遡る。
func lastTranscriptTimestamp(path string) (time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return time.Time{}, false
	}
	size := st.Size()
	offset := int64(0)
	if size > transcriptCapBytes {
		offset = size - transcriptCapBytes
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return time.Time{}, false
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return time.Time{}, false
	}
	lines := strings.Split(string(data), "\n")
	// 先頭行は cap で切った途中から始まる可能性があるので捨てる (壊れた JSON になる)。
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimSpace(lines[i])
		if ln == "" {
			continue
		}
		var e struct {
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal([]byte(ln), &e); err != nil || e.Timestamp == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339, e.Timestamp); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}
