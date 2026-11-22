// Package gameserver は UDP ソケットと internal/game の Hub を配線し、
// 受信ループとブロードキャストループを駆動するサーバー実装を提供する。
// cmd/gameserver はこのパッケージの薄いエントリポイント。
package gameserver

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"time"

	"github.com/raidync/server/internal/game"
	"github.com/raidync/server/pkg/protocol"
)

// readBufSize は 1 データグラムの読み取りバッファ。ゲームメッセージは小さい（1 データグラム = 1 メッセージ）。
const readBufSize = 2048

// Config はサーバー起動設定。
type Config struct {
	BindAddr        string   // 例: ":7777" / "127.0.0.1:0"
	SessionID       string   // ローカルモードの固定セッション ID
	QuestID         string   // ローカルモードの固定セッションのクエスト
	Roster          []string // ローカルモードの参加許可プレイヤー
	GameLiftEnabled bool     // true で GameLift がセッションを駆動（固定セッションを作らない）
	SessionAPIURL   string   // /session/end を呼ぶ API のベース URL。空なら報告しない
}

// Server は UDP ゲームサーバー 1 プロセス。
type Server struct {
	conn *net.UDPConn
	hub  *game.Hub
}

// New は設定に従って UDP をリッスンし、初期セッションを登録した Server を返す。
// hubOpts は Hub にそのまま渡す（セッション終了の通知先など）。
func New(cfg Config, hubOpts ...game.HubOption) (*Server, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", cfg.BindAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}

	hub := game.NewHub(&udpSender{conn: conn}, hubOpts...)
	// ローカルモードのみ固定セッションを登録する。
	// GameLift モードでは OnStartGameSession コールバックで動的に作るのでここでは作らない。
	if !cfg.GameLiftEnabled {
		hub.Register(game.NewSession(cfg.SessionID, cfg.QuestID, cfg.Roster, nowMs()))
	}

	return &Server{conn: conn, hub: hub}, nil
}

// LocalAddr はリッスン中のアドレスを返す（テストで ephemeral ポートを知るのに使う）。
func (s *Server) LocalAddr() *net.UDPAddr { return s.conn.LocalAddr().(*net.UDPAddr) }

// Hub は内部 Hub を返す（テスト・観測用）。
func (s *Server) Hub() *game.Hub { return s.hub }

// Serve は ctx がキャンセルされるまで受信・ブロードキャストを駆動する。ブロッキング。
func (s *Server) Serve(ctx context.Context) {
	go s.broadcastLoop(ctx)
	s.receiveLoop(ctx)
}

// Close は UDP ソケットを閉じる。
func (s *Server) Close() error { return s.conn.Close() }

// receiveLoop は UDP データグラムを読み続け、エンベロープを復号して Hub にルーティングする。
func (s *Server) receiveLoop(ctx context.Context) {
	buf := make([]byte, readBufSize)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// ctx キャンセルでも抜けられるよう読み取りに締切を入れる。
		_ = s.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			log.Printf("read error: %v", err)
			continue
		}

		var msg protocol.GameMessage
		if err := json.Unmarshal(buf[:n], &msg); err != nil {
			log.Printf("drop malformed packet from %s: %v", addr, err)
			continue
		}
		if err := s.hub.Route(&msg, addr, nowMs()); err != nil {
			log.Printf("drop packet from %s (%s): %v", addr, msg.PlayerID, err)
		}
	}
}

// broadcastLoop は 50ms 周期で Hub.Tick を呼び、SESSION_STATE 送出とライフサイクル管理を行う。
func (s *Server) broadcastLoop(ctx context.Context) {
	ticker := time.NewTicker(protocol.StateSyncIntervalMs * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.hub.Tick(nowMs())
		}
	}
}

// udpSender は *net.UDPConn を game.Sender に適合させる。
type udpSender struct {
	conn *net.UDPConn
}

func (u *udpSender) SendTo(addr *net.UDPAddr, data []byte) error {
	_, err := u.conn.WriteToUDP(data, addr)
	return err
}

func nowMs() int64 { return time.Now().UnixMilli() }
