package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/raidync/server/internal/db"
)

var (
	questID string
	players int
)

func init() {
	flag.StringVar(&questID, "questID", "", "クエストIDを指定")
	flag.IntVar(&players, "players", 0, "プレイヤー数を指定")
}

func main() {
	flag.Parse()
	ctx := context.Background()

	// 検証できない（入力不正・DB 接続失敗）は即終了。
	if questID == "" || players <= 0 || players%4 != 0 {
		log.Fatalf("invalid input: questID=%s, players=%d", questID, players)
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
	if len(sessions) != players/4 {
		log.Printf("NG: expected %d sessions, got %d", players/4, len(sessions))
		failed = true
	}

	seen := make(map[string]string) // playerId → 最初に見た sessionId
	for _, s := range sessions {
		if len(s.PlayerIDs) != 4 {
			log.Printf("NG: session %s has %d players, expected 4", s.SessionID, len(s.PlayerIDs))
			failed = true
		}
		for _, pid := range s.PlayerIDs {
			if first, exists := seen[pid]; exists {
				log.Printf("NG: player %s is in multiple sessions: %s and %s", pid, first, s.SessionID)
				failed = true
				continue
			}
			seen[pid] = s.SessionID
		}
	}

	if failed {
		os.Exit(1)
	}
	log.Printf("OK: %d sessions / %d players", len(sessions), len(seen))
}
