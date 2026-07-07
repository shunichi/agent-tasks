package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// prBrowserAction は PR があれば全 URL を返し、無ければ nil + メッセージを返す。
func TestPRBrowserAction(t *testing.T) {
	urls, msg := prBrowserAction(Task{PRs: []string{"https://example.com/pr/1", "https://example.com/pr/2"}})
	if msg != "" {
		t.Errorf("PR ありでメッセージが出た: %q", msg)
	}
	if !slices.Equal(urls, []string{"https://example.com/pr/1", "https://example.com/pr/2"}) {
		t.Errorf("urls = %v, want 2 件すべて", urls)
	}

	urls, msg = prBrowserAction(Task{})
	if urls != nil {
		t.Errorf("PR 無しで urls = %v, want nil", urls)
	}
	if msg == "" {
		t.Error("PR 無しでメッセージが出ない")
	}
}

// ヘルプに o (PR を開く) の項目がある。
func TestHelpHasOpenKey(t *testing.T) {
	found := false
	for _, e := range helpEntries() {
		if e[0] == "o" {
			found = true
			break
		}
	}
	if !found {
		t.Error("helpEntries に o (PR を開く) が無い")
	}
}

// openURLs は PATH のオープナー (ここでは stub xdg-open) に各 URL を渡して起動する。
// 実ブラウザは開かず、stub が受け取った引数をファイルに記録して検証する。
func TestOpenURLsInvokesOpener(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "opened.txt")
	// stub xdg-open: 受け取った URL を marker に追記する。
	stub := "#!/bin/sh\necho \"$1\" >> " + marker + "\n"
	if err := os.WriteFile(filepath.Join(dir, "xdg-open"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir) // stub だけを見えるようにする (実 open/xdg-open を排除)

	if _, ok := browserOpener(); !ok {
		t.Fatal("stub xdg-open が browserOpener に見つからない")
	}
	if err := openURLs([]string{"https://example.com/pr/1"}); err != nil {
		t.Fatalf("openURLs: %v", err)
	}
	// 起動は非同期なので marker ができるまで少し待つ。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(marker); err == nil && len(b) > 0 {
			if string(b) != "https://example.com/pr/1\n" {
				t.Errorf("opener に渡った URL = %q", string(b))
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("stub xdg-open が起動されなかった (marker 未作成)")
}

// オープナーが PATH に無ければエラーを返す (呼び出し側がフッターに表示)。
func TestOpenURLsNoOpener(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // 空ディレクトリ = オープナー無し
	if err := openURLs([]string{"https://example.com/pr/1"}); err == nil {
		t.Error("オープナーが無いのにエラーにならなかった")
	}
}

// sessionBrowserAction は session: が http(s) URL のときそれを返し、空/非 URL では
// nil + メッセージを返す。
func TestSessionBrowserAction(t *testing.T) {
	// http URL → 1 件返す。
	urls, msg := sessionBrowserAction(Task{Session: "https://claude.ai/code/session_01ABC"})
	if msg != "" {
		t.Errorf("URL ありでメッセージが出た: %q", msg)
	}
	if !slices.Equal(urls, []string{"https://claude.ai/code/session_01ABC"}) {
		t.Errorf("urls = %v, want session URL 1 件", urls)
	}

	// 空 → nil + 「セッション URL はありません」。
	urls, msg = sessionBrowserAction(Task{Session: ""})
	if urls != nil || msg == "" {
		t.Errorf("session 空: urls=%v msg=%q, want nil + メッセージ", urls, msg)
	}

	// 非 URL (手動で変な値) → nil + メッセージ。
	urls, msg = sessionBrowserAction(Task{Session: "not-a-url"})
	if urls != nil || msg == "" {
		t.Errorf("非 URL: urls=%v msg=%q, want nil + メッセージ", urls, msg)
	}
}

// ヘルプに O (セッション URL を開く) の項目がある。
func TestHelpHasOpenSessionKey(t *testing.T) {
	found := false
	for _, e := range helpEntries() {
		if e[0] == "O" {
			found = true
			break
		}
	}
	if !found {
		t.Error("helpEntries に O (セッション URL を開く) が無い")
	}
}
