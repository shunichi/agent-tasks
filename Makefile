# ビルドするバイナリ名。稼働中の別ビルドと共存させるため名前でパラメータ化する。
# このブランチ (herdr 対応版) は既定を agent-tasks-herdr にして、本体版 (agent-tasks) の
# symlink / 補完 / state dir を一切上書きしないようにする (0105 の「移行の制約」参照)。
# 本体版と同じ名前で入れたいときは `make NAME=agent-tasks ...`。
NAME ?= agent-tasks-herdr
BIN := bin/$(NAME)
PREFIX ?= $(HOME)/.local
# skill 名は NAME から独立して常に固定 (0118)。skill 選択は環境非依存 (関連度のみ) なので、
# 同目的の skill を2つ (agent-tasks / agent-tasks-herdr) 入れると競合が起きる。よって
# skill は常に1つ (agent-tasks) に集約し、herdr/tmux の分岐は SKILL.md 本文と、CLI 名の
# 解決 (下記ルーター) で吸収する。
SKILL_NAME := agent-tasks
# agent-tasks の実体解決ルーター (0118)。$(PREFIX)/bin より前に PATH を通しておく場所に置く
# (本体版・herdr 版どちらの `make install` も触らない場所。詳細は docs/herdr-migration.md)。
ROUTER_DIR := $(HOME)/.local/agent-tasks-router/bin
# progName をビルド名に合わせて埋め込む。state dir の分離 (session.go) と補完の
# コマンド名/関数名 (completion.go) がこの値に追従する。NAME=agent-tasks なら既定値と同じで無害。
LDFLAGS := -X main.progName=$(NAME)

.PHONY: build install link install-completions install-router clean test fmt vet

build: $(BIN)

$(BIN): $(wildcard *.go) $(wildcard templates/*/*) go.mod
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

# CLI を PATH へ、skill を ~/.claude/skills へ symlink + 補完を再生成 (ビルドも実行)。
# バイナリ/補完は $(NAME) 名で入れるので、別名ビルドは本体版の symlink/補完を上書きしない。
# skill は $(SKILL_NAME) (常に agent-tasks) で入れる (herdr/main 併存時は「最後に install した方」
# が有効になる。ドッグフード中はこのブランチの `make install` を正とする。詳細は install-router)。
# 補完は静的に書き出すファイルなので install に含める。機能追加後に make install を一度打てば
# バイナリ + skill + 補完がすべて最新になる (CLI は symlink で常に最新だが、補完だけ古いまま
# 残るのを防ぐ)。
install: build link install-completions install-router

link:
	mkdir -p $(PREFIX)/bin $(HOME)/.claude/skills
	ln -sf  $(CURDIR)/$(BIN)             $(PREFIX)/bin/$(NAME)
	ln -sfn $(CURDIR)/skills/agent-tasks $(HOME)/.claude/skills/$(SKILL_NAME)

# agent-tasks の env ルーターを導入する (0118)。HERDR_ENV=1 なら herdr 版、そうでなければ本体版を
# 実行する薄いラッパー。$(PREFIX)/bin ではなく $(ROUTER_DIR) に置くので、本体版/herdr 版どちらの
# `make install` (link) が何度動いても上書きされない。$(ROUTER_DIR) を $(PREFIX)/bin より前に
# PATH へ通しておく必要がある (一度だけの手動セットアップ。docs/herdr-migration.md 参照)。
install-router:
	mkdir -p $(ROUTER_DIR)
	install -m 0755 scripts/agent-tasks-router.sh $(ROUTER_DIR)/agent-tasks

# bash / zsh 補完スクリプトを標準的な場所へ書き出す (ビルドも実行)。ファイル名も $(NAME) 由来に
# するので本体版の補完 (agent-tasks / _agent_tasks) を上書きしない。zsh は関数名規則に合わせて
# ハイフンを _ に (agent-tasks-herdr -> _agent_tasks_herdr)。
install-completions: build
	mkdir -p $(PREFIX)/share/bash-completion/completions $(PREFIX)/share/zsh/site-functions
	$(BIN) completion bash > $(PREFIX)/share/bash-completion/completions/$(NAME)
	$(BIN) completion zsh  > $(PREFIX)/share/zsh/site-functions/_$(subst -,_,$(NAME))

fmt:
	gofmt -w *.go

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf bin
