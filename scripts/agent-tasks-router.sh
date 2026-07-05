#!/bin/sh
# agent-tasks の env ルーター (agent-tasks/0118)。
#
# herdr 内 (HERDR_ENV=1) では herdr 版 (agent-tasks-herdr) を、それ以外 (tmux 等) では
# 本体版 (agent-tasks) を実行する。$PREFIX/bin (既定 ~/.local/bin) より前に PATH へ通した
# ディレクトリに置くことで、`agent-tasks` という同じコマンド名のまま確実に振り分ける。
#
# $HOME/.local/bin/agent-tasks{,-herdr} は絶対パスで直接叩く (PATH 解決を経由しない) ので、
# 本体版・herdr 版どちらの `make install` がこのファイルを上書きすることもない。
set -eu

if [ "${HERDR_ENV:-}" = "1" ] && [ -x "$HOME/.local/bin/agent-tasks-herdr" ]; then
  exec "$HOME/.local/bin/agent-tasks-herdr" "$@"
fi

exec "$HOME/.local/bin/agent-tasks" "$@"
