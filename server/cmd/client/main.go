// client はローカル E2E を端から端まで駆動する CLI。本番 Unity クライアントの代役として
// 「ログイン → マッチ要求 → ステータス待ち → UDP プレイ → 結果報告 → 履歴確認」を 1 本で通す。
// 各処理は段階的に実装していく（まずは 3-1: ログインまで）。
//
// 接続先:
//
//	-mock-auth … mock-auth のベース URL（既定 http://localhost:9229）
//	-api       … localapi のベース URL（既定 http://localhost:8080）
//
// 認証情報:
//
//	-email / -password … ログインに使う資格情報。mock-auth はインメモリ保持のため、
//	                      未登録なら signup してから login する（409 は登録済みとして許容）。
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/raidync/server/pkg/protocol"
)

// config は CLI フラグから組み立てる実行設定。
type config struct {
	mockAuthURL string
	apiURL      string
	email       string
	password    string
	questID     string
	soloAllowed bool
	moves       int
	result      string
}

func main() {
	cfg := parseFlags()

	// ログ整形（時刻だけ付ける最小設定）。
	log.SetFlags(log.Ltime)

	ctx := context.Background()
	c := newClient(cfg)

	// 3-1: signup（409 許容）→ login。以降のステップで使う認証状態をここで確立する。
	if err := c.authenticate(ctx); err != nil {
		log.Fatalf("client: authenticate: %v", err)
	}
	log.Printf("client: authenticated playerId=%s token=%s…", c.playerID, truncate(c.idToken, 16))

	// 3-2: マッチ要求。soloAllowed=true なら単一クライアントでも即マッチ成立する。
	if err := c.requestMatch(ctx); err != nil {
		log.Fatalf("client: requestMatch: %v", err)
	}
	log.Printf("client: match requested ticketId=%s", c.ticketID)

	// 3-3: MATCHED になるまでステータスをポーリングし、接続情報を取り出す。
	if err := c.pollMatch(ctx); err != nil {
		log.Fatalf("client: pollMatch: %v", err)
	}
	log.Printf("client: matched sessionId=%s connect=%s:%d", c.sessionID, c.conn.IPAddress, c.conn.Port)

	// 3-4: UDP でゲームサーバに接続し、PLAYER_MOVE/PLAYER_ACTION を送って SESSION_STATE を受ける。
	if err := c.playSession(ctx); err != nil {
		log.Fatalf("client: playSession: %v", err)
	}

	// 3-5: 結果を報告し、MatchHistory への保存を起こす（server-to-server 経路）。
	if err := c.reportResult(ctx); err != nil {
		log.Fatalf("client: reportResult: %v", err)
	}

	// 3-6: 履歴を取得し、いま書いたエントリが返ることを確認して E2E を締める。
	if err := c.fetchHistory(ctx); err != nil {
		log.Fatalf("client: fetchHistory: %v", err)
	}
	log.Printf("client: E2E completed")
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.mockAuthURL, "mock-auth", "http://localhost:9229", "mock-auth base URL")
	flag.StringVar(&cfg.apiURL, "api", "http://localhost:8080", "localapi base URL")
	flag.StringVar(&cfg.email, "email", "player1@example.com", "login email")
	flag.StringVar(&cfg.password, "password", "password123", "login password")
	flag.StringVar(&cfg.questID, "quest", "quest-rathalos", "quest id to matchmake for")
	flag.BoolVar(&cfg.soloAllowed, "solo", true, "allow immediate solo match (single-client E2E)")
	flag.IntVar(&cfg.moves, "moves", 10, "number of PLAYER_MOVE packets to send during play")
	flag.StringVar(&cfg.result, "result", "CLEAR", "match result to report (CLEAR/FAILED/ABANDONED)")
	flag.Parse()
	return cfg
}

// client は E2E を通すための状態（認証トークン・playerId）と HTTP クライアントを保持する。
type client struct {
	cfg  config
	http *http.Client

	playerID  string                  // signup で採番された playerId（JWT sub と一致）
	idToken   string                  // 以降の localapi 呼び出しで Bearer に載せる
	ticketID  string                  // match/request で得たマッチ待ちチケット
	sessionID string                  // マッチ成立で確定したゲームセッション ID
	conn      protocol.ConnectionInfo // UDP 接続先（ゲームサーバ）
	matchedAt time.Time               // マッチ成立時刻（所要秒の起点）
}

func newClient(cfg config) *client {
	return &client{
		cfg:  cfg,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// authenticate は mock-auth に対して signup（既存なら 409 を許容）→ login を行い、
// playerID と idToken を確立する。
func (c *client) authenticate(ctx context.Context) error {
	if err := c.signup(ctx); err != nil {
		return err
	}
	return c.login(ctx)
}

type signupResponse struct {
	PlayerID string `json:"playerId"`
	Email    string `json:"email"`
}

// signup は資格情報を登録する。既に登録済み（409）なら成功扱いにする。
// 新規登録できた場合は採番された playerId を控える（login 後にトークンからも一致する）。
func (c *client) signup(ctx context.Context) error {
	body := map[string]string{"email": c.cfg.email, "password": c.cfg.password}
	status, raw, err := c.postJSON(ctx, c.cfg.mockAuthURL+"/signup", "", body)
	if err != nil {
		return fmt.Errorf("signup: %w", err)
	}
	switch status {
	case http.StatusCreated:
		var res signupResponse
		if err := json.Unmarshal(raw, &res); err != nil {
			return fmt.Errorf("signup: decode response: %w", err)
		}
		c.playerID = res.PlayerID
		log.Printf("client: signed up playerId=%s", res.PlayerID)
		return nil
	case http.StatusConflict:
		log.Printf("client: account already exists, proceeding to login")
		return nil
	default:
		return fmt.Errorf("signup: unexpected status %d: %s", status, raw)
	}
}

type loginResponse struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// login は資格情報で id_token を取得し、c.idToken に保持する。
func (c *client) login(ctx context.Context) error {
	body := map[string]string{"email": c.cfg.email, "password": c.cfg.password}
	status, raw, err := c.postJSON(ctx, c.cfg.mockAuthURL+"/login", "", body)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("login: unexpected status %d: %s", status, raw)
	}
	var res loginResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("login: decode response: %w", err)
	}
	if res.IDToken == "" {
		return fmt.Errorf("login: empty id_token")
	}
	c.idToken = res.IDToken

	// roster 照合に使う playerId は JWT の sub。signup を経ず login だけ（既存アカウント）でも
	// 確実に得られるよう、トークンから取り出して確定させる（localapi 側の sub 抽出と一致する）。
	sub, err := subjectFromJWT(res.IDToken)
	if err != nil {
		return fmt.Errorf("login: extract subject: %w", err)
	}
	c.playerID = sub
	return nil
}

// subjectFromJWT は JWT のペイロード（2 番目のセグメント）をデコードして sub を取り出す。
// ここでは署名検証はしない（検証は localapi 側の verifier が行う）。playerId 取得が目的。
func subjectFromJWT(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("not a JWT (segments=%d)", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode payload: %w", err)
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("unmarshal claims: %w", err)
	}
	if claims.Sub == "" {
		return "", fmt.Errorf("sub claim is empty")
	}
	return claims.Sub, nil
}

// requestMatch は localapi にマッチ要求を出し、得た ticketId を c.ticketID に保持する。
// 認証は id_token を Bearer に載せる（localapi 側で sub=playerId を取り出す）。
func (c *client) requestMatch(ctx context.Context) error {
	in := protocol.MatchRequestInput{
		QuestID:     c.cfg.questID,
		SoloAllowed: c.cfg.soloAllowed,
	}
	status, raw, err := c.postJSON(ctx, c.cfg.apiURL+"/match/request", c.idToken, in)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", status, raw)
	}
	var out protocol.MatchRequestOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if out.TicketID == "" {
		return fmt.Errorf("empty ticketId")
	}
	c.ticketID = out.TicketID
	log.Printf("client: ticket status=%s quest=%s", out.Status, c.cfg.questID)
	return nil
}

// pollMatch は MATCHED になるまで GET /match/status/{ticketId} を一定間隔でポーリングする。
//   - MATCHED        … sessionId と ConnectionInfo を保持して成功
//   - WAITING/MATCHING … マッチ確定待ち。間隔を空けて再試行
//   - TIMEOUT/CANCELLED … 不成立として失敗
//
// soloAllowed=true なら最初のポーリングで即 MATCHED になる想定。全体は MaxWaitSeconds
// （60s）+ 余裕で打ち切る。
func (c *client) pollMatch(ctx context.Context) error {
	const interval = 500 * time.Millisecond
	deadline := time.Now().Add(70 * time.Second)

	for {
		out, err := c.matchStatus(ctx)
		if err != nil {
			return err
		}
		switch out.Status {
		case protocol.StatusMatched:
			if out.SessionID == "" || out.ConnectionInfo == nil {
				return fmt.Errorf("matched but missing session/connection info")
			}
			c.sessionID = out.SessionID
			c.conn = *out.ConnectionInfo
			c.matchedAt = time.Now()
			return nil
		case protocol.StatusWaiting, protocol.StatusMatching:
			log.Printf("client: status=%s waited=%ds…", out.Status, out.WaitedSec)
		case protocol.StatusTimeout, protocol.StatusCancelled:
			return fmt.Errorf("match not formed: status=%s", out.Status)
		default:
			return fmt.Errorf("unexpected status: %s", out.Status)
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("poll deadline exceeded (last status %s)", out.Status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// playSession は UDP でゲームサーバに接続し、一定数の PLAYER_MOVE と 1 回の PLAYER_ACTION を
// 送りつつ、別ゴルーチンで SESSION_STATE を受信する。Seq は 1 始まりの単調増加で採番する
// （サーバ側のシーケンスゲートが古い/重複を捨てる前提）。最後に受信件数を要約する。
func (c *client) playSession(ctx context.Context) error {
	raddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(c.conn.IPAddress, strconv.Itoa(c.conn.Port)))
	if err != nil {
		return fmt.Errorf("resolve udp addr: %w", err)
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return fmt.Errorf("dial udp: %w", err)
	}
	defer conn.Close()
	log.Printf("client: udp connected to %s", raddr)

	// 受信ゴルーチン: conn が閉じられるまで SESSION_STATE / SESSION_END を読み続ける。
	var received int
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 64*1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return // conn.Close() で抜ける
			}
			var msg protocol.GameMessage
			if err := json.Unmarshal(buf[:n], &msg); err != nil {
				continue
			}
			switch msg.Type {
			case protocol.MsgSessionState:
				received++
				var pl protocol.SessionStatePayload
				if json.Unmarshal(msg.Payload, &pl) == nil && len(pl.Players) > 0 {
					log.Printf("client: <- SESSION_STATE seq=%d players=%d self.health=%d",
						msg.Seq, len(pl.Players), selfHealth(pl.Players, c.playerID))
				}
			case protocol.MsgSessionEnd:
				log.Printf("client: <- SESSION_END")
			}
		}
	}()

	// PLAYER_MOVE を 100ms 間隔で moves 回送る。位置を少しずつ進める。
	var seq uint32
	for i := 0; i < c.cfg.moves; i++ {
		seq++
		pos := protocol.Vector3{X: float64(i), Y: 0, Z: float64(i) * 0.5}
		payload, _ := json.Marshal(protocol.PlayerMovePayload{
			Position: pos,
			Rotation: protocol.Vector3{},
			State:    protocol.ActorMoving,
		})
		if err := c.sendUDP(conn, seq, protocol.MsgPlayerMove, payload); err != nil {
			return err
		}
		if !sleepCtx(ctx, 100*time.Millisecond) {
			break
		}
	}

	// 仕上げに PLAYER_ACTION（攻撃）を 1 回。ソロなので対象は付けず attacking 状態のみ動かす。
	seq++
	actionPayload, _ := json.Marshal(protocol.PlayerActionPayload{Action: "attack"})
	if err := c.sendUDP(conn, seq, protocol.MsgPlayerAction, actionPayload); err != nil {
		return err
	}

	// ブロードキャスト（20Hz）を少し受けてから締める。
	sleepCtx(ctx, 500*time.Millisecond)
	_ = conn.Close()
	<-done

	if received == 0 {
		return fmt.Errorf("no SESSION_STATE received (server may not have registered roster)")
	}
	log.Printf("client: play done sent=%d moves +1 action, received=%d SESSION_STATE", c.cfg.moves, received)
	return nil
}

// sendUDP は GameMessage エンベロープを組んで 1 データグラム送る。
func (c *client) sendUDP(conn *net.UDPConn, seq uint32, t protocol.GameMessageType, payload json.RawMessage) error {
	msg := protocol.GameMessage{
		Type:      t,
		SessionID: c.sessionID,
		PlayerID:  c.playerID,
		Timestamp: time.Now().UnixMilli(),
		Seq:       seq,
		Payload:   payload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", t, err)
	}
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", t, err)
	}
	return nil
}

// selfHealth は SESSION_STATE のプレイヤー一覧から自分の health を引く（見つからなければ -1）。
func selfHealth(players []protocol.PlayerState, playerID string) int {
	for _, p := range players {
		if p.PlayerID == playerID {
			return p.Health
		}
	}
	return -1
}

// sleepCtx は d 待つ。ctx がキャンセルされたら false を返して即時打ち切る。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// reportResult は POST /session/{sessionId}/end で結果を報告する。本番ではゲームサーバ
// （GameLift プロセス）が呼ぶ server-to-server 経路のため、プレイヤー JWT は載せない。
// DurationSec はマッチ成立からの経過秒。これにより MatchHistory への保存が走る。
func (c *client) reportResult(ctx context.Context) error {
	in := protocol.SessionEndInput{
		Result:      protocol.MatchResult(c.cfg.result),
		DurationSec: int(time.Since(c.matchedAt).Seconds()),
	}
	url := c.cfg.apiURL + "/session/" + c.sessionID + "/end"
	status, raw, err := c.postJSON(ctx, url, "", in)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", status, raw)
	}
	var out protocol.SessionEndOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	log.Printf("client: result reported status=%s playersSaved=%d result=%s",
		out.Status, out.PlayersSaved, c.cfg.result)
	return nil
}

// fetchHistory は GET /player/{playerId}/history を叩き、いま報告した結果が
// 履歴に反映されているか確認する。直近エントリが今回の sessionId と一致すれば成功。
func (c *client) fetchHistory(ctx context.Context) error {
	url := c.cfg.apiURL + "/player/" + c.playerID + "/history"
	status, raw, err := c.getJSON(ctx, url, c.idToken)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", status, raw)
	}
	var out protocol.MatchHistoryOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	log.Printf("client: history entries=%d", len(out.Entries))
	for i, e := range out.Entries {
		log.Printf("client:   [%d] session=%s quest=%s result=%s duration=%ds",
			i, e.SessionID, e.QuestID, e.Result, e.DurationSec)
	}

	// 今回のセッションが履歴に含まれることを E2E 成立の確証とする。
	for _, e := range out.Entries {
		if e.SessionID == c.sessionID {
			return nil
		}
	}
	return fmt.Errorf("reported session %s not found in history", c.sessionID)
}

// matchStatus は GET /match/status/{ticketId} を 1 回叩いて結果をデコードする。
func (c *client) matchStatus(ctx context.Context) (protocol.MatchStatusOutput, error) {
	url := c.cfg.apiURL + "/match/status/" + c.ticketID
	status, raw, err := c.getJSON(ctx, url, c.idToken)
	if err != nil {
		return protocol.MatchStatusOutput{}, err
	}
	if status != http.StatusOK {
		return protocol.MatchStatusOutput{}, fmt.Errorf("unexpected status %d: %s", status, raw)
	}
	var out protocol.MatchStatusOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return protocol.MatchStatusOutput{}, fmt.Errorf("decode response: %w", err)
	}
	return out, nil
}

// getJSON は GET リクエストを送り、ステータスコードと生レスポンスを返す。
func (c *client) getJSON(ctx context.Context, url, bearer string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("new request: %w", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read response: %w", err)
	}
	return resp.StatusCode, raw, nil
}

// postJSON は body を JSON 送信し、ステータスコードと生レスポンスを返す。
// bearer が空でなければ Authorization ヘッダを付ける（localapi 呼び出し用）。
func (c *client) postJSON(ctx context.Context, url, bearer string, body any) (int, []byte, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return 0, nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read response: %w", err)
	}
	return resp.StatusCode, raw, nil
}

// truncate は先頭 n 文字（バイト）だけ返す。トークンのログ出力で全体を晒さないため。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
