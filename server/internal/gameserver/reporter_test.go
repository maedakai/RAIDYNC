package gameserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raidync/server/pkg/protocol"
)

// fakeSessionAPI は /session/{id}/end の代わりに、決めた順にステータスを返し、受けたリクエストを記録する。
// 用意した数より多く呼ばれたら、最後のステータスを返し続ける。
type fakeSessionAPI struct {
	mu       sync.Mutex
	statuses []int
	calls    int
	paths    []string
	bodies   []protocol.SessionEndInput
}

func (f *fakeSessionAPI) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var in protocol.SessionEndInput
	_ = json.NewDecoder(req.Body).Decode(&in)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, req.Method+" "+req.URL.Path)
	f.bodies = append(f.bodies, in)
	status := f.statuses[min(f.calls, len(f.statuses)-1)]
	f.calls++
	w.WriteHeader(status)
}

// reportOnce は偽 API に向けた SessionReporter で 1 回報告し、終わるまで待つ。
func reportOnce(t *testing.T, statuses ...int) *fakeSessionAPI {
	t.Helper()
	api := &fakeSessionAPI{statuses: statuses}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	r := NewSessionReporter(srv.URL)
	r.backoff = time.Millisecond // 送り直しの間隔を縮めてテストを速くする
	r.OnSessionEnd("sess-1", protocol.EndTimeout, 1200)
	r.Wait()
	return api
}

func TestSessionReporter_Success(t *testing.T) {
	api := reportOnce(t, http.StatusOK)

	require.Equal(t, 1, api.calls)
	assert.Equal(t, "POST /session/sess-1/end", api.paths[0])
	assert.Equal(t, protocol.SessionEndInput{Result: protocol.ResultAbandoned, DurationSec: 1200}, api.bodies[0])
}

func TestSessionReporter_RetriesServerError(t *testing.T) {
	api := reportOnce(t, http.StatusInternalServerError, http.StatusOK)
	assert.Equal(t, 2, api.calls, "1 回目の 500 の後に送り直し、200 で止まる")
}

func TestSessionReporter_GivesUpAfterMaxAttempts(t *testing.T) {
	api := reportOnce(t, http.StatusInternalServerError)
	assert.Equal(t, reportAttempts, api.calls, "500 が続いても上限で止まる")
}

func TestSessionReporter_DoesNotRetryConflict(t *testing.T) {
	api := reportOnce(t, http.StatusConflict)
	assert.Equal(t, 1, api.calls, "409 は送り直しても変わらないので 1 回で終える")
}

func TestResultFor(t *testing.T) {
	tests := []struct {
		reason protocol.SessionEndReason
		want   protocol.MatchResult
	}{
		{protocol.EndCompleted, protocol.ResultClear},
		{protocol.EndTimeout, protocol.ResultAbandoned},
		{protocol.EndAbandoned, protocol.ResultAbandoned},
	}
	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			assert.Equal(t, tt.want, resultFor(tt.reason))
		})
	}
}
