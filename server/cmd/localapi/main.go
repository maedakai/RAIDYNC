// localapi はローカル E2E 用の HTTP サーバ。API Gateway + 各 Lambda の代役として、
// internal/handlers の Handle 群を net/http にマウントする。本番経路（cmd/lambda/*）と
// ハンドラ本体を共有し、差し替えるのはトランスポートと依存（LocalStack エンドポイント等）のみ。
//
// 環境変数:
//
//	LOCALAPI_ADDR     … リッスンアドレス（既定 :8080）
//	DYNAMODB_ENDPOINT … LocalStack の DynamoDB（例 http://localhost:4566）
//	AWS_REGION        … 既定 ap-northeast-1
//	AUTH_JWKS_URL     … mock-auth の JWKS（例 http://localhost:9229/.well-known/jwks.json）
//	AUTH_ISSUER       … トークン issuer（例 http://localhost:9229）
//	AUTH_CLIENT_ID    … 任意（空なら aud/client_id チェックをしない）
package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/raidync/server/internal/auth"
	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/internal/handlers"
	"github.com/raidync/server/internal/matchmaking"
	"github.com/raidync/server/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type server struct {
	verifier *auth.Verifier
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx := context.Background()

	shutdown, err := telemetry.Init(ctx, "localapi")
	if err != nil {
		log.Fatalf("localapi: init telemetry: %v", err)
	}

	client, err := db.NewClient(ctx)
	if err != nil {
		log.Fatalf("localapi: init db client: %v", err)
	}

	verifier, err := auth.New(ctx, auth.Config{
		JWKSURL:  mustEnv("AUTH_JWKS_URL"),
		Issuer:   mustEnv("AUTH_ISSUER"),
		ClientID: os.Getenv("AUTH_CLIENT_ID"),
	})
	if err != nil {
		log.Fatalf("localapi: init verifier: %v", err)
	}

	s := &server{verifier: verifier}

	// SIGINT/SIGTERM 連動のシャットダウン文脈。HTTP とゲームサーバの双方で共有する。
	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// UDP ゲームサーバを同居起動し、その Hub に登録する SessionProvider を得る。
	// これがマッチ成立時のセッション生成（本番 GameLift の代役）を担う。
	bindAddr := getenv("GAMESERVER_BIND_ADDR", "127.0.0.1:7777")
	advertiseHost := getenv("GAMESERVER_ADVERTISE_HOST", "127.0.0.1")
	provider, gameSrv, err := startGameServer(shutdownCtx, bindAddr, advertiseHost)
	if err != nil {
		log.Fatalf("localapi: start gameserver: %v", err)
	}
	defer gameSrv.Close()
	log.Printf("localapi: gameserver(udp) listening on %s (advertise %s:%d)",
		gameSrv.LocalAddr(), advertiseHost, gameSrv.LocalAddr().Port)

	// ハンドラは本番 Lambda と同じものを生成（依存だけ LocalStack / 同居 Hub 向けに）。
	matchRequest := handlers.NewMatchRequest(client)
	matchStatus := handlers.NewMatchStatus(matchmaking.NewMatcher(client, provider))
	matchCancel := handlers.NewMatchCancel(client)
	matchHistory := handlers.NewMatchHistory(client)
	sessionEnd := handlers.NewSessionEnd(client)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// /match・/player 配下は本番の authorizer Lambda 相当（JWT 検証 → sub 注入）を前段に置く。
	handleTraced(mux, "POST /match/request", s.withAuth(adapt(matchRequest.Handle)))
	handleTraced(mux, "GET /match/status/{ticketId}", s.withAuth(adapt(matchStatus.Handle, "ticketId")))
	handleTraced(mux, "DELETE /match/cancel/{ticketId}", s.withAuth(adapt(matchCancel.Handle, "ticketId")))
	handleTraced(mux, "GET /player/{playerId}/history", s.withAuth(adapt(matchHistory.Handle, "playerId")))

	// /session/end は server-to-server 経路（ゲームサーバが呼ぶ）。本番ではフリートの
	// IAM ロール／内部ネットワークで保護する前提のため、プレイヤー JWT は要求しない。
	handleTraced(mux, "POST /session/{sessionId}/end", adapt(sessionEnd.Handle, "sessionId"))

	addr := getenv("LOCALAPI_ADDR", ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           logging(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("localapi: listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("localapi: serve: %v", err)
		}
	}()

	<-shutdownCtx.Done()
	log.Printf("localapi: shutting down")
	timeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(timeout); err != nil {
		log.Printf("localapi: shutdown: %v", err)
	}
	// HTTP を止めた後に、溜まった Span を送り切る（逆順だと HTTP 終了中の Span が欠ける）。
	if err := shutdown(timeout); err != nil {
		log.Printf("localapi: shutdown telemetry: %v", err)
	}
}

// withAuth は Bearer JWT を検証し、成功時に sub を Authorizer Context へ注入する。
// 本番では API Gateway の authorizer Lambda が担う役割をローカルで肩代わりする。
func (s *server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := bearer(r.Header.Get("Authorization"))
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "missing or malformed bearer token")
			return
		}
		claims, err := s.verifier.Verify(token)
		if err != nil {
			log.Printf("localapi: verify failed: %v", err)
			writeJSONError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		authz := map[string]interface{}{"sub": claims.PlayerID}
		ctx := context.WithValue(r.Context(), authCtxKey, authz)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bearer は "Authorization: Bearer <jwt>" から jwt 部分を取り出す。
func bearer(raw string) (string, error) {
	parts := strings.SplitN(raw, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", errors.New("malformed authorization header")
	}
	return parts[1], nil
}

// logging は 1 リクエスト 1 行のアクセスログを出す最小ミドルウェア。
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("localapi: %s %s (%s)", r.Method, r.URL.Path, time.Since(start))
	})
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("localapi: %s is required", key)
	}
	return v
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// handleTraced は mux.Handle と同じくルートを登録し、OTel の HTTP 計測で包む。
// Span 名にはルートのパターンを使う（パスそのままだと ID ごとに名前が分かれてしまうため）。
func handleTraced(mux *http.ServeMux, pattern string, handler http.Handler) {
	mux.Handle(pattern, otelhttp.NewHandler(handler, pattern))
}
