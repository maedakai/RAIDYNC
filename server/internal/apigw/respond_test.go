package apigw

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
)

func TestErrorEmitCondition(t *testing.T) {
	// slog の出力先を一時的にバッファへ差し替え、fn の実行中に出た行を返す。
	capture := func(t *testing.T, fn func()) string {
		t.Helper()
		var buf bytes.Buffer
		old := slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
		defer slog.SetDefault(old)
		fn()
		return buf.String()
	}

	t.Run("400 なら何も出ない", func(t *testing.T) {
		out := capture(t, func() {
			Error(context.Background(), 400, "invalid request body", errors.New("boom"))
		})

		if out != "" {
			t.Errorf("400なのに出力がある = %q", out)
		}
	})
	t.Run("err が nil なら何も出ない", func(t *testing.T) {
		out := capture(t, func() {
			Error(context.Background(), 500, "ticket lookup failed", nil)
		})

		if out != "" {
			t.Errorf("errがnilなのに出力がある = %q", out)
		}
	})
	t.Run("500 かつ err ありなら出力される", func(t *testing.T) {
		out := capture(t, func() {
			Error(context.Background(), 500, "ticket lookup failed", errors.New("boom"))
		})
		if out == "" {
			t.Error("500 なのに出力が無い")
		}
	})
}
