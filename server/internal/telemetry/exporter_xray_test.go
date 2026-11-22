package telemetry

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// roundTripFunc は関数を http.RoundTripper として渡すための偽物。next に入れ、署名後のリクエストを横取りする。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestSigV4RoundTripper は、送信直前のコピーに署名が付き、本文が欠けずに届き、元のリクエストは書き換えないことを確かめる。
// 署名の値そのものの正しさ（X-Ray が受け付けるか）はダミーの認証情報では確かめられないため、AWS で確認する。
func TestSigV4RoundTripper(t *testing.T) {
	tests := []struct {
		name string
		body []byte // nil は Body の無いリクエスト
	}{
		{name: "本文なし", body: nil},
		{name: "本文あり", body: []byte(`{"resourceSpans":[]}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 1. 偽物を作る：受け取った req を外の変数に記録し、空の 200 応答を返す
			var got *http.Request
			var gotBody []byte
			next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				got = req
				if req.Body != nil {
					gotBody, _ = io.ReadAll(req.Body)
				}
				return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
			})

			// 2. sigv4RoundTripper を組み立てる（next に偽物、creds にダミー）
			rt := &sigv4RoundTripper{
				next:   next,
				signer: v4.NewSigner(),
				creds:  credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""),
				region: "ap-northeast-1",
			}

			// 3. 元のリクエストを作り、rt.RoundTrip(元) を呼ぶ
			var r io.Reader
			if tt.body != nil {
				r = bytes.NewReader(tt.body)
			}
			req, err := http.NewRequest("POST", "https://xray.ap-northeast-1.amazonaws.com/v1/traces", r)
			require.NoError(t, err)

			_, err = rt.RoundTrip(req)
			require.NoError(t, err)

			// 4. 3 点を確かめる
			// ① 署名が付いた：SigV4 の Authorization ヘッダーで、サービス名と region が入っている
			auth := got.Header.Get("Authorization")
			assert.True(t, strings.HasPrefix(auth, "AWS4-HMAC-SHA256"), auth)
			assert.Contains(t, auth, "/ap-northeast-1/xray/aws4_request")

			// ② 本文がそのまま届いた（本文なしなら両方 nil）
			assert.Equal(t, tt.body, gotBody)

			// ③ 元のリクエストには署名が付いていない（コピーに署名している）
			assert.Empty(t, req.Header.Get("Authorization"))
		})
	}
}
