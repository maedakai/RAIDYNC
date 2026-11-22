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

// QueryByPlayer は playerId の履歴を新しい順（timestamp 降順）に limit 件返す。
func (c *Client) QueryByPlayer(ctx context.Context, playerID string, limit int32) ([]protocol.MatchHistoryEntry, error) {
	out, err := c.ddb.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(c.tables.MatchHistory),
		KeyConditionExpression: aws.String("playerId = :p"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":p": &ddbtypes.AttributeValueMemberS{Value: playerID},
		},
		ScanIndexForward: aws.Bool(false), // SK 降順 = 新しい順
		Limit:            aws.Int32(limit),
	})
	if err != nil {
		return nil, err
	}
	entries := make([]protocol.MatchHistoryEntry, 0, len(out.Items))
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &entries); err != nil {
		return nil, fmt.Errorf("unmarshal history: %w", err)
	}
	return entries, nil
}
