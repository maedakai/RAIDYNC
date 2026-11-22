package telemetry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// xraySigningName は X-Ray の OTLP エンドポイントに送るときの SigV4 のサービス名。
const xraySigningName = "xray"

// sigv4RoundTripper は送信直前のリクエストに SigV4 署名を付けてから next に渡す。
// X-Ray の OTLP エンドポイントは署名の無いリクエストを拒否するが、otlptracehttp は AWS 用ではなく
// 自分では署名しないため、http.Client の Transport に挟んで署名を足す（C# の DelegatingHandler に相当）。
type sigv4RoundTripper struct {
	next   http.RoundTripper
	signer *v4.Signer
	creds  aws.CredentialsProvider // 送るたびに Retrieve する。Lambda の一時認証情報の更新は Provider 側がキャッシュ込みで行う
	region string
}

// RoundTrip は http.RoundTripper の実装。受け取った req は書き換えず、コピーに署名して送る。
// 署名には本文の SHA-256 が要るが、Body は一度読むと空になるため、読んだ内容をコピーの Body に詰め直す。
func (t *sigv4RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	clone := req.Clone(ctx)

	var body []byte
	if req.Body != nil {
		// RoundTripper は元の Body を（エラー時も含め）必ず閉じる決まり。
		defer req.Body.Close()
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		body = b
		clone.Body = io.NopCloser(bytes.NewReader(body))
	}
	sum := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(sum[:])

	creds, err := t.creds.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieve aws credentials: %w", err)
	}
	if err := t.signer.SignHTTP(ctx, creds, clone, payloadHash, xraySigningName, t.region, time.Now()); err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}
	return t.next.RoundTrip(clone)
}
