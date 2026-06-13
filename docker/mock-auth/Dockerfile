# syntax=docker/dockerfile:1.7

# mock-auth は Cognito のローカル代役。AWS にはデプロイされず、
# 開発者ローカルと CI でしか動かないため、dev 単一ステージで十分。
# 本番デプロイ対象（Lambda / GameLift）側でマルチステージ + distroless を使う。
FROM golang:1.26-alpine AS dev

WORKDIR /app

# air を導入（ホットリロード用、再現性のためバージョン固定）
RUN go install github.com/air-verse/air@v1.65.3

# 起動時のモジュール解決を速くするため、先にキャッシュを温める
COPY go.mod go.sum ./
RUN go mod download

EXPOSE 9229

# ソースは docker-compose の bind mount で /app に注入される前提
CMD ["air", "-c", ".air.toml"]
