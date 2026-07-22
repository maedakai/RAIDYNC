#!/usr/bin/env bash
# GameLift Managed EC2 フリートへアップロードする Build 一式を作る。
#
# 出力: build/gameserver/
#   gameserver  … Go バイナリ（linux/amd64）
#   install.sh  … Build 展開後に GameLift が自動実行する。実行権限を付け直す
#   start.sh    … 起動ラッパー。runtime_configuration の launch_path が指す
#
# なぜ start.sh が要るか:
#   GameLift の runtime configuration には「環境変数を渡す」機能が無く、渡せるのは
#   コマンドライン引数(parameters)だけ。cmd/gameserver は GAMELIFT_ENABLED /
#   GAMESERVER_BIND を環境変数で読むため、export してから exec するラッパーを噛ませる。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_DIR="$REPO_ROOT/server"
OUT_DIR="$REPO_ROOT/build/gameserver"

# GameLift のインスタンスタイプ c5.large は x86_64（spec-architecture.md §2.2）。
# Lambda(arm64) とは異なるので注意。
export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

echo "building gameserver -> ${OUT_DIR}/gameserver"
(cd "$SERVER_DIR" && go build -trimpath -ldflags="-s -w" -o "$OUT_DIR/gameserver" ./cmd/gameserver)

# GameLift は Build 展開後に install.sh を実行する。zip は実行権限を保持しないことが
# あるため、ここで確実に付け直す。
cat > "$OUT_DIR/install.sh" <<'EOF'
#!/bin/sh
set -e
chmod +x /local/game/gameserver /local/game/start.sh
EOF

# 起動ラッパー。GAMELIFT_ENABLED=true で GameLiftBridge を有効化し、
# SDK ライフサイクル（InitSDK→ProcessReady→OnStartGameSession…）に入る。
# Managed EC2 では SDK の接続情報が実行環境に自動で与えられるため、
# Anywhere のような AuthToken / WebSocket URL の設定は不要。
cat > "$OUT_DIR/start.sh" <<'EOF'
#!/bin/sh
set -e
export GAMELIFT_ENABLED=true
export GAMESERVER_BIND=:7777
exec /local/game/gameserver
EOF

chmod +x "$OUT_DIR/install.sh" "$OUT_DIR/start.sh"

echo "done: ${OUT_DIR}"
ls -l "$OUT_DIR"
