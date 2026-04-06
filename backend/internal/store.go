package internal

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

type Token struct {
	Token     string `dynamodbav:"token"`
	S3Key     string `dynamodbav:"s3_key"`
	Note      string `dynamodbav:"note,omitempty"`
	CreatedAt string `dynamodbav:"created_at"`
	TTL       int64  `dynamodbav:"ttl"`
}

type Session struct {
	SessionID     string `dynamodbav:"session_id"`
	S3Key         string `dynamodbav:"s3_key"`
	Note          string `dynamodbav:"note,omitempty"`
	CreatedAt     string `dynamodbav:"created_at"`
	LastHeartbeat string `dynamodbav:"last_heartbeat"`
	PauseTimeout  int    `dynamodbav:"pause_timeout_seconds"`
	Status        string `dynamodbav:"status"`
	TTL           int64  `dynamodbav:"ttl"`
}

type Store struct {
	DDB            *dynamodb.Client
	S3             *s3.Client
	S3Presign      *s3.PresignClient
	TokensTable    string
	SessionsTable  string
	AudioBucket    string
	PauseTimeout   int
}

func NewStore(ctx context.Context) (*Store, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	timeout := 15
	if v := os.Getenv("PAUSE_TIMEOUT"); v != "" {
		timeout, _ = strconv.Atoi(v)
	}

	s3Client := s3.NewFromConfig(cfg)
	return &Store{
		DDB:           dynamodb.NewFromConfig(cfg),
		S3:            s3Client,
		S3Presign:     s3.NewPresignClient(s3Client),
		TokensTable:   os.Getenv("TOKENS_TABLE"),
		SessionsTable: os.Getenv("SESSIONS_TABLE"),
		AudioBucket:   os.Getenv("AUDIO_BUCKET"),
		PauseTimeout:  timeout,
	}, nil
}

func NewStoreWith(ddb *dynamodb.Client, s3c *s3.Client, tokensTable, sessionsTable, audioBucket string) *Store {
	return &Store{
		DDB:           ddb,
		S3:            s3c,
		S3Presign:     s3.NewPresignClient(s3c),
		TokensTable:   tokensTable,
		SessionsTable: sessionsTable,
		AudioBucket:   audioBucket,
		PauseTimeout:  15,
	}
}

// PresignUpload generates a pre-signed PUT URL for direct client→S3 upload.
func (s *Store) PresignUpload(ctx context.Context, s3Key, contentType string) (string, error) {
	req, err := s.S3Presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.AudioBucket,
		Key:         &s3Key,
		ContentType: &contentType,
	}, s3.WithPresignExpires(15*time.Minute))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// PresignStream generates a pre-signed GET URL for direct S3→client streaming.
func (s *Store) PresignStream(ctx context.Context, s3Key string) (string, error) {
	req, err := s.S3Presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.AudioBucket,
		Key:    &s3Key,
	}, s3.WithPresignExpires(1*time.Hour))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (s *Store) CreateToken(ctx context.Context, tokenID, s3Key, note string) (Token, error) {
	if tokenID == "" {
		tokenID = uuid.New().String()
	}

	now := time.Now()
	tok := Token{
		Token:     tokenID,
		S3Key:     s3Key,
		Note:      note,
		CreatedAt: now.UTC().Format(time.RFC3339),
		TTL:       now.Add(2 * 24 * time.Hour).Unix(),
	}

	item, err := attributevalue.MarshalMap(tok)
	if err != nil {
		return Token{}, err
	}

	// Conditional put — fail if token already exists
	_, err = s.DDB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           &s.TokensTable,
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(#t)"),
		ExpressionAttributeNames: map[string]string{
			"#t": "token",
		},
	})
	if err != nil {
		return Token{}, fmt.Errorf("token already taken")
	}
	return tok, nil
}

// BurnToken atomically deletes the token and creates a session.
// Returns the session, or error if the token doesn't exist.
func (s *Store) BurnToken(ctx context.Context, tokenID string) (Session, error) {
	now := time.Now()

	// Get and delete the token atomically
	result, err := s.DDB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &s.TokensTable,
		Key: map[string]ddbtypes.AttributeValue{
			"token": &ddbtypes.AttributeValueMemberS{Value: tokenID},
		},
		ConditionExpression: aws.String("attribute_exists(#t)"),
		ExpressionAttributeNames: map[string]string{
			"#t": "token",
		},
		ReturnValues: ddbtypes.ReturnValueAllOld,
	})
	if err != nil {
		return Session{}, fmt.Errorf("token not found")
	}

	var tok Token
	if err := attributevalue.UnmarshalMap(result.Attributes, &tok); err != nil {
		return Session{}, err
	}

	sess := Session{
		SessionID:     uuid.New().String(),
		S3Key:         tok.S3Key,
		Note:          tok.Note,
		CreatedAt:     now.UTC().Format(time.RFC3339),
		LastHeartbeat: now.UTC().Format(time.RFC3339),
		PauseTimeout:  s.PauseTimeout,
		Status:        "active",
		TTL:           now.Add(1 * time.Hour).Unix(),
	}

	item, err := attributevalue.MarshalMap(sess)
	if err != nil {
		return Session{}, err
	}

	_, err = s.DDB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.SessionsTable,
		Item:      item,
	})
	return sess, err
}

func (s *Store) GetSession(ctx context.Context, sessionID string) (Session, error) {
	result, err := s.DDB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &s.SessionsTable,
		Key: map[string]ddbtypes.AttributeValue{
			"session_id": &ddbtypes.AttributeValueMemberS{Value: sessionID},
		},
	})
	if err != nil {
		return Session{}, err
	}
	if result.Item == nil {
		return Session{}, fmt.Errorf("session not found")
	}

	var sess Session
	if err := attributevalue.UnmarshalMap(result.Item, &sess); err != nil {
		return Session{}, err
	}

	// Check if pause timeout has expired
	if sess.Status == "active" {
		lastHB, _ := time.Parse(time.RFC3339, sess.LastHeartbeat)
		if time.Since(lastHB) > time.Duration(sess.PauseTimeout)*time.Second {
			_ = s.ExpireSession(ctx, sessionID, sess.S3Key)
			return Session{}, fmt.Errorf("session not found")
		}
	}

	if sess.Status != "active" {
		return Session{}, fmt.Errorf("session not found")
	}

	return sess, nil
}

func (s *Store) Heartbeat(ctx context.Context, sessionID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.DDB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &s.SessionsTable,
		Key: map[string]ddbtypes.AttributeValue{
			"session_id": &ddbtypes.AttributeValueMemberS{Value: sessionID},
		},
		UpdateExpression:    aws.String("SET last_heartbeat = :hb"),
		ConditionExpression: aws.String("#s = :active"),
		ExpressionAttributeNames: map[string]string{
			"#s": "status",
		},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":hb":     &ddbtypes.AttributeValueMemberS{Value: now},
			":active": &ddbtypes.AttributeValueMemberS{Value: "active"},
		},
	})
	return err
}

func (s *Store) CompleteSession(ctx context.Context, sessionID string) error {
	// Get session to find S3 key
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	return s.ExpireSession(ctx, sessionID, sess.S3Key)
}

func (s *Store) ExpireSession(ctx context.Context, sessionID, s3Key string) error {
	// Mark session as completed/expired
	_, err := s.DDB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &s.SessionsTable,
		Key: map[string]ddbtypes.AttributeValue{
			"session_id": &ddbtypes.AttributeValueMemberS{Value: sessionID},
		},
		UpdateExpression: aws.String("SET #s = :done"),
		ExpressionAttributeNames: map[string]string{
			"#s": "status",
		},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":done": &ddbtypes.AttributeValueMemberS{Value: "completed"},
		},
	})
	if err != nil {
		return err
	}

	// Delete audio from S3
	_, err = s.S3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.AudioBucket,
		Key:    &s3Key,
	})
	return err
}
