package gameserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/raidync/server/pkg/protocol"
)

const (
	// reportTimeout は /session/end への 1 回の HTTP の待ち時間の上限。
	reportTimeout = 20 * time.Second
	// reportAttempts は送る回数の上限（最初の 1 回 + 送り直し 2 回）。
	reportAttempts = 3
	// reportBackoff は送り直しの最初の間隔。1 → 2 秒と倍にしていく。
	reportBackoff = 1 * time.Second
)

// SessionReporter は game.SessionEndObserver の実装で、セッションが終わったら
// POST /session/{sessionId}/end で結果を報告する。
// OnSessionEnd は Hub の Tick から呼ばれるため、HTTP は別のゴルーチンで行いすぐ戻る。
// プロセスを終える前に Wait で送りかけの報告を待つ。
type SessionReporter struct {
	apiURL  string
	client  *http.Client
	backoff time.Duration // 送り直しの最初の間隔。テストでは短くする
	wg      sync.WaitGroup
}

// NewSessionReporter は apiURL（例: https://api.example.com）へ報告する SessionReporter を返す。
func NewSessionReporter(apiURL string) *SessionReporter {
	return &SessionReporter{
		apiURL:  apiURL,
		client:  &http.Client{Timeout: reportTimeout},
		backoff: reportBackoff,
	}
}

// OnSessionEnd は報告を別のゴルーチンで始め、すぐ戻る。
// wg.Go はゴルーチンを起動する前に数を足すため、直後に Wait が走っても待ち漏れない。
func (r *SessionReporter) OnSessionEnd(sessionID string, reason protocol.SessionEndReason, durationSec int) {
	r.wg.Go(func() {
		r.report(sessionID, protocol.SessionEndInput{
			Result:      resultFor(reason),
			DurationSec: durationSec,
		})
	})
}

// Wait は送りかけの報告がすべて終わるまで待つ。
func (r *SessionReporter) Wait() {
	r.wg.Wait()
}

// report は 1 セッション分の報告を、必要なら送り直しながら行う。
//   - 200: 完了
//   - 409（ENDED / FAILED）・404: 送り直しても変わらないので、ログを残して終わる
//   - 5xx・ネットワークの失敗: 送り直す。/session/end は履歴と遷移を 1 つのトランザクションで書くため、二重には書かれない
func (r *SessionReporter) report(sessionID string, in protocol.SessionEndInput) {
	body, err := json.Marshal(in)
	if err != nil {
		slog.Error("session end report: marshal failed", "sessionId", sessionID, "error", err)
		return
	}
	endpoint := r.apiURL + "/session/" + url.PathEscape(sessionID) + "/end"

	wait := r.backoff
	for attempt := 1; attempt <= reportAttempts; attempt++ {
		status, err := r.post(endpoint, body)
		switch {
		case err == nil && status == http.StatusOK:
			slog.Info("session end reported", "sessionId", sessionID, "result", in.Result, "durationSec", in.DurationSec)
			return
		case err == nil && status == http.StatusConflict:
			slog.Info("session end report: session is not active", "sessionId", sessionID)
			return
		case err == nil && status == http.StatusNotFound:
			slog.Warn("session end report: session not found", "sessionId", sessionID)
			return
		case err == nil && status < 500:
			// 400 など。送り直しても変わらない
			slog.Error("session end report: rejected", "sessionId", sessionID, "status", status)
			return
		}

		if attempt == reportAttempts {
			slog.Error("session end report failed", "sessionId", sessionID, "attempts", attempt, "status", status, "error", err)
			return
		}
		slog.Warn("session end report: retrying", "sessionId", sessionID, "attempt", attempt, "status", status, "error", err)
		time.Sleep(wait)
		wait *= 2
	}
}

// post は body を JSON として POST し、ステータスコードを返す。
func (r *SessionReporter) post(endpoint string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// resultFor はゲームの終わり方を、報告する結果に対応させる。
// 最後まで遊んだ completed だけを CLEAR とし、時間切れ・誰も来ない・全員抜けたは ABANDONED にする。
func resultFor(reason protocol.SessionEndReason) protocol.MatchResult {
	if reason == protocol.EndCompleted {
		return protocol.ResultClear
	}
	return protocol.ResultAbandoned
}
