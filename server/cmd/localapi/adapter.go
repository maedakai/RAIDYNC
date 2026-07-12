// adapter は net/http と API Gateway プロキシ統合の橋渡しをする。
// localapi は本番 Lambda と同じ internal/handlers.*.Handle を呼ぶため、
// ここで *http.Request → events.APIGatewayProxyRequest、その応答 → http.ResponseWriter
// の相互変換を行う（ロジックは一切持たない純粋な変換層）。
package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
)

// proxyHandler は internal/handlers の各 Handle メソッドの共通シグネチャ。
type proxyHandler func(context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error)

// ctxKey は context への値埋め込み用の専用キー型（衝突防止）。
type ctxKey string

const authCtxKey ctxKey = "authorizer"

// adapt は proxyHandler を http.Handler に変換する。
// pathParamKeys には ServeMux のパターンで使ったパス変数名を渡す（例: "ticketId"）。
func adapt(h proxyHandler, pathParamKeys ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "failed to read body")
			return
		}

		headers := make(map[string]string, len(r.Header))
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}

		pathParams := make(map[string]string, len(pathParamKeys))
		for _, key := range pathParamKeys {
			pathParams[key] = r.PathValue(key)
		}

		req := events.APIGatewayProxyRequest{
			HTTPMethod:     r.Method,
			Path:           r.URL.Path,
			Headers:        headers,
			Body:           string(body),
			PathParameters: pathParams,
			RequestContext: events.APIGatewayProxyRequestContext{
				Authorizer: authorizerFromContext(r.Context()),
			},
		}

		resp, err := h(r.Context(), req)
		if err != nil {
			// 本番でも Handle は error を返さずステータスで表現する設計だが、保険。
			log.Printf("localapi: handler error: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}

		for k, v := range resp.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(resp.StatusCode)
		if _, err := io.WriteString(w, resp.Body); err != nil {
			log.Printf("localapi: write response: %v", err)
		}
	}
}

// authorizerFromContext は auth ミドルウェアが埋めた Authorizer Context を取り出す。
// 未認証ルートでは nil（apigw.PlayerID が X-Player-Id にフォールバックする）。
func authorizerFromContext(ctx context.Context) map[string]interface{} {
	if v, ok := ctx.Value(authCtxKey).(map[string]interface{}); ok {
		return v
	}
	return nil
}

// writeJSONError は apigw.Error と同形の {"message": ...} を返す。
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}
