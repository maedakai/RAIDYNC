package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/raidync/server/pkg/protocol"
)

// ErrConflict は楽観ロック条件に失敗した（他のマッチャーが先に確保した／状態が変わった）ことを示す。
var ErrConflict = errors.New("ticket state conflict")

const (
	indexQuestStatus  = "questId-status-index"
	indexPlayerStatus = "playerId-status-index"
)

// PutTicket は新規チケットを登録する（呼び出し側で WAITING・version=0 を設定済みの想定）。
func (c *Client) PutTicket(ctx context.Context, t protocol.Ticket) error {
	item, err := attributevalue.MarshalMap(t)
	if err != nil {
		return fmt.Errorf("marshal ticket: %w", err)
	}
	_, err = c.ddb.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(c.tables.MatchmakingTickets),
		Item:      item,
	})
	return err
}

// GetTicket は ticketId で 1 件取得する。存在しなければ (nil, nil)。
func (c *Client) GetTicket(ctx context.Context, ticketID string) (*protocol.Ticket, error) {
	out, err := c.ddb.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(c.tables.MatchmakingTickets),
		Key:       ticketKey(ticketID),
	})
	if err != nil {
		return nil, err
	}
	if out.Item == nil {
		return nil, nil
	}
	var t protocol.Ticket
	if err := attributevalue.UnmarshalMap(out.Item, &t); err != nil {
		return nil, fmt.Errorf("unmarshal ticket: %w", err)
	}
	return &t, nil
}

// QueryWaitingByQuest は questId-status-index で同 quest の WAITING チケットを集める。
func (c *Client) QueryWaitingByQuest(ctx context.Context, questID string, limit int32) ([]protocol.Ticket, error) {
	out, err := c.ddb.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(c.tables.MatchmakingTickets),
		IndexName:              aws.String(indexQuestStatus),
		KeyConditionExpression: aws.String("questId = :q AND #s = :w"),
		ExpressionAttributeNames: map[string]string{
			"#s": "status", // status は DynamoDB 予約語のためプレースホルダ必須
		},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":q": &ddbtypes.AttributeValueMemberS{Value: questID},
			":w": &ddbtypes.AttributeValueMemberS{Value: string(protocol.StatusWaiting)},
		},
		Limit: aws.Int32(limit),
	})
	if err != nil {
		return nil, err
	}
	return unmarshalTickets(out.Items)
}

// FindWaitingByPlayer は playerId-status-index で同 player の WAITING チケットを 1 件探す。
// 「1 プレイヤー 1 チケット」の冪等性検証に使う。なければ (nil, nil)。
func (c *Client) FindWaitingByPlayer(ctx context.Context, playerID string) (*protocol.Ticket, error) {
	out, err := c.ddb.Query(ctx, &dynamodb.QueryInput{
		TableName:                aws.String(c.tables.MatchmakingTickets),
		IndexName:                aws.String(indexPlayerStatus),
		KeyConditionExpression:   aws.String("playerId = :p AND #s = :w"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":p": &ddbtypes.AttributeValueMemberS{Value: playerID},
			":w": &ddbtypes.AttributeValueMemberS{Value: string(protocol.StatusWaiting)},
		},
		Limit: aws.Int32(1),
	})
	if err != nil {
		return nil, err
	}
	tickets, err := unmarshalTickets(out.Items)
	if err != nil || len(tickets) == 0 {
		return nil, err
	}
	return &tickets[0], nil
}

// ClaimTickets は対象チケット群を version 一致条件付きで WAITING → MATCHING に原子的に遷移する。
// 1 件でも条件不一致なら全体が失敗し ErrConflict を返す（他マッチャーが先に確保した合図）。
func (c *Client) ClaimTickets(ctx context.Context, tickets []protocol.Ticket) error {
	if len(tickets) == 0 {
		return nil
	}
	writes := make([]ddbtypes.TransactWriteItem, 0, len(tickets))
	for _, t := range tickets {
		writes = append(writes, ddbtypes.TransactWriteItem{
			Update: &ddbtypes.Update{
				TableName:           aws.String(c.tables.MatchmakingTickets),
				Key:                 ticketKey(t.TicketID),
				UpdateExpression:    aws.String("SET #s = :matching, #v = :next"),
				ConditionExpression: aws.String("#s = :waiting AND #v = :cur"),
				ExpressionAttributeNames: map[string]string{
					"#s": "status",
					"#v": "version",
				},
				ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
					":matching": &ddbtypes.AttributeValueMemberS{Value: string(protocol.StatusMatching)},
					":waiting":  &ddbtypes.AttributeValueMemberS{Value: string(protocol.StatusWaiting)},
					":cur":      &ddbtypes.AttributeValueMemberN{Value: strconv.Itoa(t.Version)},
					":next":     &ddbtypes.AttributeValueMemberN{Value: strconv.Itoa(t.Version + 1)},
				},
			},
		})
	}
	_, err := c.ddb.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: writes,
	})
	if err != nil {
		var canceled *ddbtypes.TransactionCanceledException
		if errors.As(err, &canceled) {
			return ErrConflict
		}
		return err
	}
	return nil
}

// MarkMatched は確保済み（MATCHING）チケットを MATCHED に遷移し、接続情報を書き込む。
func (c *Client) MarkMatched(ctx context.Context, ticketID, sessionID string, conn protocol.ConnectionInfo) error {
	connAV, err := attributevalue.Marshal(conn)
	if err != nil {
		return fmt.Errorf("marshal connectionInfo: %w", err)
	}
	_, err = c.ddb.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                aws.String(c.tables.MatchmakingTickets),
		Key:                      ticketKey(ticketID),
		UpdateExpression:         aws.String("SET #s = :matched, sessionId = :sid, connectionInfo = :conn"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":matched": &ddbtypes.AttributeValueMemberS{Value: string(protocol.StatusMatched)},
			":sid":     &ddbtypes.AttributeValueMemberS{Value: sessionID},
			":conn":    connAV,
		},
	})
	return err
}

// CancelTicket は WAITING のチケットのみ CANCELLED に遷移する。
// MATCHING / MATCHED では ErrConflict（409 相当）を返す。
func (c *Client) CancelTicket(ctx context.Context, ticketID string) error {
	return c.transitionFromWaiting(ctx, ticketID, protocol.StatusCancelled)
}

// TimeoutTicket は WAITING のチケットのみ TIMEOUT に遷移する（60 秒経過時）。
// 既に確保済みなら ErrConflict（無視してよい）。
func (c *Client) TimeoutTicket(ctx context.Context, ticketID string) error {
	return c.transitionFromWaiting(ctx, ticketID, protocol.StatusTimeout)
}

// transitionFromWaiting は WAITING を条件に指定状態へ遷移する共通処理。
func (c *Client) transitionFromWaiting(ctx context.Context, ticketID string, to protocol.TicketStatus) error {
	_, err := c.ddb.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                aws.String(c.tables.MatchmakingTickets),
		Key:                      ticketKey(ticketID),
		UpdateExpression:         aws.String("SET #s = :to"),
		ConditionExpression:      aws.String("#s = :waiting"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":to":      &ddbtypes.AttributeValueMemberS{Value: string(to)},
			":waiting": &ddbtypes.AttributeValueMemberS{Value: string(protocol.StatusWaiting)},
		},
	})
	if err != nil {
		var failed *ddbtypes.ConditionalCheckFailedException
		if errors.As(err, &failed) {
			return ErrConflict
		}
		return err
	}
	return nil
}

func ticketKey(ticketID string) map[string]ddbtypes.AttributeValue {
	return map[string]ddbtypes.AttributeValue{
		"ticketId": &ddbtypes.AttributeValueMemberS{Value: ticketID},
	}
}

func unmarshalTickets(items []map[string]ddbtypes.AttributeValue) ([]protocol.Ticket, error) {
	tickets := make([]protocol.Ticket, 0, len(items))
	if err := attributevalue.UnmarshalListOfMaps(items, &tickets); err != nil {
		return nil, fmt.Errorf("unmarshal tickets: %w", err)
	}
	return tickets, nil
}
