package main

import (
	"context"
	"embed"
	"path"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

//go:embed static/*
var static embed.FS

var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "application/javascript; charset=utf-8",
}

func handler(_ context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	reqPath := req.RawPath
	if reqPath == "" {
		reqPath = "/"
	}

	// Known static files
	var filename string
	switch reqPath {
	case "/style.css":
		filename = "static/style.css"
	case "/ephemeral.js":
		filename = "static/ephemeral.js"
	case "/api", "/api/", "/api/docs":
		filename = "static/api.html"
	default:
		// Everything else serves index.html (SPA routing)
		filename = "static/index.html"
	}

	data, err := static.ReadFile(filename)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{StatusCode: 500}, nil
	}

	ext := path.Ext(filename)
	ct := contentTypes[ext]
	if ct == "" {
		ct = "text/plain"
	}

	// Cache static assets briefly while iterating; HTML stays no-cache
	cacheControl := "no-cache"
	if !strings.HasSuffix(filename, ".html") {
		cacheControl = "public, max-age=60"
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type":  ct,
			"Cache-Control": cacheControl,
		},
		Body: string(data),
	}, nil
}

func main() {
	lambda.Start(handler)
}
