package main

import (
	"context"

	"ephemeral-backend/internal"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
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

	sessionID := req.PathParameters["session_id"]
	if sessionID == "" {
		return internal.NotFound(), nil
	}

	sess, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return internal.NotFound(), nil
	}

	// Generate a short-lived pre-signed GET URL
	streamURL, err := store.PresignStream(ctx, sess.S3Key)
	if err != nil {
		return internal.Error(500, "failed to generate stream URL"), nil
	}

	return internal.JSON(200, map[string]string{
		"stream_url": streamURL,
	}), nil
}

func main() {
	lambda.Start(handler)
}
