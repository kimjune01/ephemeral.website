package main

import (
	"context"

	"ephemeral-backend/internal"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var store *internal.Store

func handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if store == nil {
		var err error
		store, err = internal.NewStore(ctx)
		if err != nil {
			return internal.Error(500, "internal error"), nil
		}
	}

	tokenID := req.PathParameters["token"]
	if tokenID == "" {
		return internal.NotFound(), nil
	}

	result, err := store.DDB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &store.TokensTable,
		Key: map[string]ddbtypes.AttributeValue{
			"token": &ddbtypes.AttributeValueMemberS{Value: tokenID},
		},
	})
	if err != nil || result.Item == nil {
		return internal.JSON(200, map[string]string{"exists": "false"}), nil
	}

	return internal.JSON(200, map[string]string{"exists": "true"}), nil
}

func main() {
	lambda.Start(handler)
}
