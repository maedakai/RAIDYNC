package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
)

func TestErrorKindOf(t *testing.T) {

	tests := []struct {
		name     string
		message  string
		fallback string
		want     string
	}{
		{
			name:     "空白をアンダースコアに変換",
			message:  "ticket lookup failed",
			fallback: "unknown",
			want:     "ticket_lookup_failed",
		},
		{
			name:     "空白をアンダースコアに変換＋大文字を小文字に正規化",
			message:  "Ticket Lookup Failed",
			fallback: "unknown",
			want:     "ticket_lookup_failed",
		},
		{
			name:     "空文字のフォールバック検証",
			message:  "",
			fallback: "unknown",
			want:     "unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errorKindOf(tt.message, tt.fallback); got != tt.want {
				t.Errorf("ErrorKindOf() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEmitError(t *testing.T) {
	capture := func(t *testing.T, fn func()) string {
		t.Helper()
		var buf bytes.Buffer
		old := slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
		defer slog.SetDefault(old)
		fn()
		return buf.String()
	}
	t.Run("500 なら EMF を含む 1 行が出る", func(t *testing.T) {
		t.Setenv("AWS_LAMBDA_FUNCTION_NAME", "raidync-dev-match-request")

		out := capture(t, func() {
			EmitError(context.Background(), 500, "ticket lookup failed", errors.New("boom"))
		})

		var got map[string]any
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("出力が JSON として不正: %v (out=%q)", err, out)
		}

		// ルート直下のディメンション値とメトリクス値
		if got["FunctionName"] != "raidync-dev-match-request" {
			t.Errorf("FunctionName = %v", got["FunctionName"])
		}
		if got["ErrorKind"] != "ticket_lookup_failed" {
			t.Errorf("ErrorKind = %v", got["ErrorKind"])
		}
		if got["HandlerError"] != float64(1) {
			t.Errorf("HandlerError = %v (%T), want 1 (float64)", got["HandlerError"], got["HandlerError"])
		}

		// _aws ブロック
		aws, ok := got["_aws"].(map[string]any)
		if !ok {
			t.Fatalf("_aws が オブジェクトでない: %v", got["_aws"])
		}
		if _, ok := aws["Timestamp"].(float64); !ok {
			t.Errorf("Timestamp が数値でない: %v", aws["Timestamp"])
		}
		// CloudWatchMetrics は固定値なので、JSON に戻して丸ごと比較する。
		// キー名の綴り・Dimensions がキー名であること・Metrics の内容を一度に検証できる。
		b, err := json.Marshal(aws["CloudWatchMetrics"])
		if err != nil {
			t.Fatalf("再シリアライズ失敗: %v", err)
		}
		want := `[{"Dimensions":[["FunctionName"],["FunctionName","ErrorKind"]],"Metrics":[{"Name":"HandlerError","Unit":"Count"}],"Namespace":"RAIDYNC/Handlers"}]`
		if string(b) != want {
			t.Errorf("CloudWatchMetrics =\n %s\nwant\n %s", b, want)
		}
	})
	t.Run("環境変数が未設定なら localapi", func(t *testing.T) {
		t.Setenv("AWS_LAMBDA_FUNCTION_NAME", "")
		out := capture(t, func() {
			EmitError(context.Background(), 500, "ticket lookup failed", errors.New("boom"))
		})

		var got map[string]any
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("出力が JSON として不正: %v (out=%q)", err, out)
		}

		// ルート直下のディメンション値とメトリクス値
		if got["FunctionName"] != "localapi" {
			t.Errorf("FunctionName = %v", got["FunctionName"])
		}
	})
}

func TestMatchFormed(t *testing.T) {
	capture := func(t *testing.T, fn func()) string {
		t.Helper()
		var buf bytes.Buffer
		old := slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
		defer slog.SetDefault(old)
		fn()
		return buf.String()
	}
	t.Run("PartySizeが文字列", func(t *testing.T) {
		out := capture(t, func() {
			MatchFormed(context.Background(), "test-quest", 4, 1000)
		})

		var got map[string]any
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("出力が JSON として不正: %v (out=%q)", err, out)
		}

		// partySize は文字列として出力されることを確認する。
		if got["PartySize"] != "4" {
			t.Errorf("PartySize = %v", got["PartySize"])
		}
		// MatichWaitTime は数値として出力されることを確認する。
		if got["MatchWaitTime"] != float64(1000) {
			t.Errorf("MatchWaitTime = %v", got["MatchWaitTime"])
		}
		aws, ok := got["_aws"].(map[string]any)
		if !ok {
			t.Fatalf("_aws が オブジェクトでない: %v", got["_aws"])
		}
		if _, ok := aws["Timestamp"].(float64); !ok {
			t.Errorf("Timestamp が数値でない: %v", aws["Timestamp"])
		}
		b, err := json.Marshal(aws["CloudWatchMetrics"])
		if err != nil {
			t.Fatalf("再シリアライズ失敗: %v", err)
		}
		want := `[{"Dimensions":[["PartySize"],["QuestID","PartySize"]],"Metrics":[{"Name":"MatchFormed","Unit":"Count"},{"Name":"MatchWaitTime","Unit":"Milliseconds"}],"Namespace":"RAIDYNC/Matchmaking"}]`
		if string(b) != want {
			t.Errorf("CloudWatchMetrics =\n %s\nwant\n %s", b, want)
		}
	})
}

func TestMatchTimeout(t *testing.T) {
	capture := func(t *testing.T, fn func()) string {
		t.Helper()
		var buf bytes.Buffer
		old := slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
		defer slog.SetDefault(old)
		fn()
		return buf.String()
	}

	t.Run("EMF スキーマが仕様どおり", func(t *testing.T) {
		out := capture(t, func() {
			MatchTimeout(context.Background(), "test-quest")
		})

		var got map[string]any
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("出力が JSON として不正: %v (out=%q)", err, out)
		}

		// ディメンション値は文字列、メトリクス値は数値で出ること。
		if got["QuestID"] != "test-quest" {
			t.Errorf("QuestID = %v (%T)", got["QuestID"], got["QuestID"])
		}
		if got["MatchTimeout"] != float64(1) {
			t.Errorf("MatchTimeout = %v (%T), want 1 (float64)", got["MatchTimeout"], got["MatchTimeout"])
		}

		aws, ok := got["_aws"].(map[string]any)
		if !ok {
			t.Fatalf("_aws がオブジェクトでない: %v", got["_aws"])
		}
		if _, ok := aws["Timestamp"].(float64); !ok {
			t.Errorf("Timestamp が数値でない: %v", aws["Timestamp"])
		}

		// MatchFormed と違いディメンションセットは 1 つだけ。ここが崩れると
		// QuestID 別の集計ができなくなるため、丸ごと文字列で固定する。
		b, err := json.Marshal(aws["CloudWatchMetrics"])
		if err != nil {
			t.Fatalf("再シリアライズ失敗: %v", err)
		}
		want := `[{"Dimensions":[["QuestID"]],"Metrics":[{"Name":"MatchTimeout","Unit":"Count"}],"Namespace":"RAIDYNC/Matchmaking"}]`
		if string(b) != want {
			t.Errorf("CloudWatchMetrics = %s, want %s", b, want)
		}
	})

	t.Run("エラーではないので ERROR で出さない", func(t *testing.T) {
		out := capture(t, func() {
			MatchTimeout(context.Background(), "test-quest")
		})

		var got map[string]any
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("出力が JSON として不正: %v (out=%q)", err, out)
		}

		// level=ERROR は「サーバー側の異常」の印として使っている（qa-log §14）。
		// マッチのタイムアウトを ERROR で出すと障害調査の邪魔になる。
		if got["level"] != "INFO" {
			t.Errorf("level = %v, want INFO", got["level"])
		}
	})
}
