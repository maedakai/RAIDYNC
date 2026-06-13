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

// PutPlayer はプレイヤープロファイルを登録／上書きする。
func (c *Client) PutPlayer(ctx context.Context, p protocol.Player) error {
	item, err := attributevalue.MarshalMap(p)
	if err != nil {
		return fmt.Errorf("marshal player: %w", err)
	}
	_, err = c.ddb.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(c.tables.Players),
		Item:      item,
	})
	return err
}

// GetPlayer は playerId で 1 件取得する。存在しなければ (nil, nil)。
func (c *Client) GetPlayer(ctx context.Context, playerID string) (*protocol.Player, error) {
	out, err := c.ddb.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(c.tables.Players),
		Key: map[string]ddbtypes.AttributeValue{
			"playerId": &ddbtypes.AttributeValueMemberS{Value: playerID},
		},
	})
	if err != nil {
		return nil, err
	}
	if out.Item == nil {
		return nil, nil
	}
	var p protocol.Player
	if err := attributevalue.UnmarshalMap(out.Item, &p); err != nil {
		return nil, fmt.Errorf("unmarshal player: %w", err)
	}
	return &p, nil
}
