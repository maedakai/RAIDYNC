#!/usr/bin/env bash
# Lambda 用 Go バイナリを AWS Lambda カスタムランタイム（provided.al2023）向けにビルドする。
#
# 出力: build/lambda/<function>/bootstrap
#   provided.al2023 は実行ファイル名が "bootstrap" である必要がある（固定）。
#   terraform 側は modules/lambda の archive_file がこのディレクトリを zip 化して参照する。
#
# 使い方:
#   ./scripts/build-lambdas.sh
#   terraform -chdir=terraform/environments/dev plan   # ビルド後でないと archive_file が失敗する
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_DIR="$REPO_ROOT/server"
OUT_ROOT="$REPO_ROOT/build/lambda"

# cmd/lambda/<name> と 1:1 で対応する。追加時はここにも足すこと。
FUNCTIONS=(
  authorizer
  match_request
  match_status
  match_cancel
  match_history
  session_end
)

# arm64(Graviton) を選ぶ理由: x86_64 より単価が安く、Go はクロスビルドで問題なく動く。
# CGO_ENABLED=0 で静的リンクにし、実行環境のライブラリ差異を排除する。
export GOOS=linux
export GOARCH=arm64
export CGO_ENABLED=0

rm -rf "$OUT_ROOT"
for fn in "${FUNCTIONS[@]}"; do
  out_dir="$OUT_ROOT/$fn"
  mkdir -p "$out_dir"
  echo "building ${fn} -> ${out_dir}/bootstrap"
  # -trimpath: バイナリに絶対パスを埋めない / -s -w: デバッグ情報を削りサイズ削減（コールドスタート短縮）
  (cd "$SERVER_DIR" && go build -trimpath -ldflags="-s -w" -o "$out_dir/bootstrap" "./cmd/lambda/${fn}")
done

echo "done: ${OUT_ROOT}"
