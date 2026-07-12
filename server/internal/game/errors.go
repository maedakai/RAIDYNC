package game

import "errors"

var (
	// ErrNotInRoster は roster に含まれない playerId からのメッセージを示す。
	ErrNotInRoster = errors.New("player not in session roster")
	// ErrSessionEnded は終了済みセッションへの取り込みを示す。
	ErrSessionEnded = errors.New("session already ended")
	// ErrUnsupportedInbound は受信を想定しないメッセージ種別を示す。
	ErrUnsupportedInbound = errors.New("unsupported inbound message type")
	// ErrSessionNotFound は未知の sessionId を示す。
	ErrSessionNotFound = errors.New("session not found")
)
