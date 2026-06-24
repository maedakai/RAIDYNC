// Package handlers は API Gateway プロキシ統合のハンドラ本体を、トランスポート非依存な
// 形で提供する。本番の cmd/lambda/* は lambda.Start にこれらの Handle を渡すだけの薄い殻、
// ローカル E2E の cmd/localapi は net/http から同じ Handle を呼び出す（共通の唯一実装）。
package handlers

import "os"

// region は環境変数 AWS_REGION（未設定なら東京）を返す。チケットの region 付与に使う。
func region() string {
	if v := os.Getenv("AWS_REGION"); v != "" {
		return v
	}
	return "ap-northeast-1"
}
