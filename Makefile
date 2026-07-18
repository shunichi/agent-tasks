# ビルドするバイナリ名。既定は本採用名 agent-tasks。名前でパラメータ化してあるので、稼働中の
# 別ビルドと一時的に共存させたいときだけ `make NAME=agent-tasks-xxx ...` で別名ビルドできる
# (herdr 移行中の共存 (0113/0118) の名残。本採用後は既定の agent-tasks で使う)。
NAME ?= agent-tasks
BIN := bin/$(NAME)
PREFIX ?= $(HOME)/.local
# codex 用 skill の設置先。codex は ~/.agents/skills を skill 探索先とする運用に移行したので
# $(AGENTS_HOME)/skills 配下に置く。CODEX_HOME (既定 ~/.codex) は「過去に codex を使った形跡」の
# 検出と、旧設置先 ($CODEX_HOME/skills) に残る symlink の後始末にのみ使う。
AGENTS_HOME ?= $(HOME)/.agents
CODEX_HOME  ?= $(HOME)/.codex
# progName をビルド名に合わせて埋め込む。state dir (session.go) と補完のコマンド名/関数名
# (completion.go) がこの値に追従する。既定 NAME=agent-tasks は var の既定値と同じで無害。
LDFLAGS := -X main.progName=$(NAME)

.PHONY: build install link link-codex install-completions clean test test-go test-js fmt vet

build: $(BIN)

# webassets/* は //go:embed で serve のフロントエンドとしてバイナリに取り込まれるのでビルド依存に含める。
$(BIN): $(wildcard *.go) $(wildcard templates/*/*) $(wildcard webassets/*.html webassets/*.css webassets/*.js) go.mod
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

# CLI を PATH へ、skill を Claude / codex 双方へ symlink + 補完を再生成 (ビルドも実行)。
# 補完は静的に書き出すファイルなので install に含める。機能追加後に make install を一度打てば
# バイナリ + skill + 補完がすべて最新になる (CLI は symlink で常に最新だが、補完だけ古いまま
# 残るのを防ぐ)。
install: build link install-completions

link: link-codex
	mkdir -p $(PREFIX)/bin $(HOME)/.claude/skills
	ln -sf  $(CURDIR)/$(BIN)             $(PREFIX)/bin/$(NAME)
	ln -sfn $(CURDIR)/skills/agent-tasks $(HOME)/.claude/skills/agent-tasks

# codex 用 skill の symlink。Claude と同一の SKILL.md を単一の情報源として共有する
# (フォーマットは互換)。設置先は ~/.agents/skills (codex の新しい探索先)。codex 未導入のマシンで
# 空の ~/.agents を作らないよう、codex バイナリがあるか ~/.agents / ~/.codex が既に在る (= 過去に
# codex を使った形跡) ときだけ張る。旧設置先 ($CODEX_HOME/skills) に自分が張った symlink が残って
# いれば、skill の二重登録を避けるため撤去する (実ディレクトリは触らない = symlink のときだけ)。
link-codex:
	@if command -v codex >/dev/null 2>&1 || [ -d "$(AGENTS_HOME)" ] || [ -d "$(CODEX_HOME)" ]; then \
		mkdir -p "$(AGENTS_HOME)/skills"; \
		ln -sfn $(CURDIR)/skills/agent-tasks "$(AGENTS_HOME)/skills/agent-tasks"; \
		echo "linked codex skill -> $(AGENTS_HOME)/skills/agent-tasks"; \
		if [ -L "$(CODEX_HOME)/skills/agent-tasks" ]; then \
			rm -f "$(CODEX_HOME)/skills/agent-tasks"; \
			echo "removed legacy codex skill link -> $(CODEX_HOME)/skills/agent-tasks"; \
		fi; \
	else \
		echo "codex not detected; skip codex skill link"; \
	fi

# bash / zsh 補完スクリプトを標準的な場所へ書き出す (ビルドも実行)。ファイル名も $(NAME) 由来に
# する (別名ビルド時に本体の補完を上書きしない)。zsh は関数名規則に合わせてハイフンを _ に。
install-completions: build
	mkdir -p $(PREFIX)/share/bash-completion/completions $(PREFIX)/share/zsh/site-functions
	$(BIN) completion bash > $(PREFIX)/share/bash-completion/completions/$(NAME)
	$(BIN) completion zsh  > $(PREFIX)/share/zsh/site-functions/_$(subst -,_,$(NAME))

fmt:
	gofmt -w *.go

vet:
	go vet ./...

# Go と埋め込みフロントエンド JS の両方をテストする。
test: test-go test-js

test-go:
	go test ./...

# serve のフロントエンド JS (webassets/) の純粋ロジックを vitest でテストする。
# 依存は最小限方針だが、JS ロジックは Go から検証できないため webassets 配下だけ pnpm を使う
# (詳細は webassets/package.json / worktime_parallel.go)。node_modules 未取得なら先に
# `cd webassets && pnpm install` する。
test-js:
	cd webassets && pnpm test

clean:
	rm -rf bin
