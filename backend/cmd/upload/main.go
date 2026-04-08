package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"ephemeral-backend/internal"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/google/uuid"
)

const maxNoteLen = 280

var slugPattern = regexp.MustCompile(`^[a-z0-9\-]{1,64}$`)

var store *internal.Store

type uploadRequest struct {
	Slug        string `json:"slug"`
	Note        string `json:"note"`
	Waveform    string `json:"waveform"`
	ContentType string `json:"content_type"`
	S3Key       string `json:"s3_key,omitempty"`
}

// audio/<uuid> — only allow reuse of S3 keys we generated ourselves
var s3KeyPattern = regexp.MustCompile(`^audio/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if store == nil {
		var err error
		store, err = internal.NewStore(ctx)
		if err != nil {
			return internal.Error(500, "internal error"), nil
		}
	}

	var body uploadRequest
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		return internal.Error(400, "invalid JSON body"), nil
	}

	if body.ContentType == "" || !strings.HasPrefix(body.ContentType, "audio/") {
		return internal.Error(400, "content_type must be audio/*"), nil
	}

	// Validate slug server-side
	if body.Slug != "" && !slugPattern.MatchString(body.Slug) {
		return internal.Error(400, "slug must be lowercase alphanumeric with hyphens, max 64 chars"), nil
	}

	if len(body.Note) > maxNoteLen {
		body.Note = body.Note[:maxNoteLen]
	}

	// Reuse S3 key if client supplied one (optimistic upload), otherwise generate
	s3Key := body.S3Key
	if s3Key != "" {
		if !s3KeyPattern.MatchString(s3Key) {
			return internal.Error(400, "invalid s3_key format"), nil
		}
	} else {
		s3Key = fmt.Sprintf("audio/%s", uuid.New().String())
	}

	// Create token first (fails fast on slug collision)
	tok, err := store.CreateToken(ctx, body.Slug, s3Key, body.Note, body.Waveform)
	if err != nil {
		if err.Error() == "token already taken" {
			return internal.Error(409, "slug already taken"), nil
		}
		return internal.Error(500, "failed to create token"), nil
	}

	// Generate pre-signed PUT URL for direct upload to S3
	uploadURL, err := store.PresignUpload(ctx, s3Key, body.ContentType)
	if err != nil {
		return internal.Error(500, "failed to generate upload URL"), nil
	}

	return internal.JSON(201, map[string]string{
		"token":      tok.Token,
		"url":        fmt.Sprintf("/%s", tok.Token),
		"upload_url": uploadURL,
		"s3_key":     s3Key,
	}), nil
}

func main() {
	lambda.Start(handler)
}
