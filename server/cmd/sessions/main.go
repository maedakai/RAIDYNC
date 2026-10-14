package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/pkg/protocol"
)

var (
	questID string
	matched int
)

func init() {
	flag.StringVar(&questID, "questID", "", "クエストIDを指定")
	flag.IntVar(&matched, "matched", 0, "マッチ成功回数（k6 の matched の passes）を指定")
}

func main() {
	flag.Parse()
	ctx := context.Background()

	// 検証できない（入力不正・DB 接続失敗）は即終了。
	if questID == "" || matched <= 0 {
		log.Fatalf("invalid input: questID=%s, matched=%d", questID, matched)
	}

	client, err := db.NewClient(ctx)
	if err != nil {
		log.Fatalf("init db client: %v", err)
	}
	sessions, err := client.ScanByQuest(ctx, questID)
	if err != nil {
		log.Fatalf("scan by quest: %v", err)
	}

	// 検証した結果の NG は全部出してから終了する（同じバグの複数の症状を一度に見るため）。
	failed := false

	// ループでは同じプレイヤーが複数セッションに入るのが正常なため、重複は見ない。
	// マッチ成功 1 回は必ず 1 枠に対応するので、枠の合計（全セッションの人数の和）が matched と一致するかを見る。
	// 二重マッチ（1 チケットが 2 セッションに入る）があると、枠の合計が matched より多くなる。
	// 人数は 60 秒後の見切り発車があるため MinMatchSize〜MaxPartySize。ソロ指定はしないので 1 人は NG。
	slots := 0
	for _, s := range sessions {
		if len(s.PlayerIDs) < protocol.MinMatchSize || len(s.PlayerIDs) > protocol.MaxPartySize {
			log.Printf("NG: session %s has %d players, expected %d-%d", s.SessionID, len(s.PlayerIDs), protocol.MinMatchSize, protocol.MaxPartySize)
			failed = true
		}
		slots += len(s.PlayerIDs)
	}

	if slots != matched {
		log.Printf("NG: expected %d slots, got %d", matched, slots)
		failed = true
	}

	if failed {
		os.Exit(1)
	}
	log.Printf("OK: %d sessions / %d matched", len(sessions), matched)
}
