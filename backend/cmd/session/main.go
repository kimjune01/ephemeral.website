package main

import (
	"context"
	"encoding/json"

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

	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil || body.Token == "" {
		return internal.NotFound(), nil
	}

	sess, err := store.BurnToken(ctx, body.Token)
	if err != nil {
		return internal.NotFound(), nil
	}

	resp := map[string]any{
		"session_id":    sess.SessionID,
		"pause_timeout": sess.PauseTimeout,
	}
	if sess.Note != "" {
		resp["note"] = sess.Note
	}

	return internal.JSON(201, resp), nil
}

func main() {
	lambda.Start(handler)
}
