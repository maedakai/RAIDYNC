package db

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/raidync/server/pkg/protocol"
)

// PutSession はゲームセッションを登録する（マッチ成立時に作成）。
func (c *Client) PutSession(ctx context.Context, s protocol.Session) error {
	item, err := attributevalue.MarshalMap(s)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	_, err = c.ddb.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(c.tables.Sessions),
		Item:      item,
	})
	return err
}

// GetSession は sessionId で 1 件取得する。存在しなければ (nil, nil)。
func (c *Client) GetSession(ctx context.Context, sessionID string) (*protocol.Session, error) {
	out, err := c.ddb.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(c.tables.Sessions),
		Key:       sessionKey(sessionID),
	})
	if err != nil {
		return nil, err
	}
	if out.Item == nil {
		return nil, nil
	}
	var s protocol.Session
	if err := attributevalue.UnmarshalMap(out.Item, &s); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &s, nil
}

// EndSession はセッションを ENDED に遷移し、終了時刻を記録する。
func (c *Client) EndSession(ctx context.Context, sessionID string, endedAt int64) error {
	_, err := c.ddb.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                aws.String(c.tables.Sessions),
		Key:                      sessionKey(sessionID),
		UpdateExpression:         aws.String("SET #s = :ended, endedAt = :t"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":ended": &ddbtypes.AttributeValueMemberS{Value: string(protocol.SessionEnded)},
			":t":     &ddbtypes.AttributeValueMemberN{Value: itoa64(endedAt)},
		},
	})
	return err
}

func sessionKey(sessionID string) map[string]ddbtypes.AttributeValue {
	return map[string]ddbtypes.AttributeValue{
		"sessionId": &ddbtypes.AttributeValueMemberS{Value: sessionID},
	}
}
