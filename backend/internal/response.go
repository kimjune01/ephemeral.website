package internal

import (
	"encoding/json"

	"github.com/aws/aws-lambda-go/events"
)

func JSON(status int, body any) events.APIGatewayV2HTTPResponse {
	b, _ := json.Marshal(body)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: status,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(b),
	}
}

func NotFound() events.APIGatewayV2HTTPResponse {
	return events.APIGatewayV2HTTPResponse{StatusCode: 404}
}

func Error(status int, msg string) events.APIGatewayV2HTTPResponse {
	return JSON(status, map[string]string{"error": msg})
}
