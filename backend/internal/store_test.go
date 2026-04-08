package internal

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

func setupTestStore(t *testing.T) *Store {
	t.Helper()

	endpoint := "http://localhost:8000"
	cfg := aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("fake", "fake", "fake"),
	}

	ddbClient := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = &endpoint
	})

	s3Endpoint := "http://localhost:9000"
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = &s3Endpoint
		o.UsePathStyle = true
	})

	ctx := context.Background()
	suffix := uuid.New().String()[:8]
	tokensTable := "test-tokens-" + suffix
	sessionsTable := "test-sessions-" + suffix
	audioBucket := "test-audio"

	_, err := ddbClient.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   &tokensTable,
		BillingMode: ddbtypes.BillingModePayPerRequest,
		KeySchema: []ddbtypes.KeySchemaElement{
			{AttributeName: aws.String("token"), KeyType: ddbtypes.KeyTypeHash},
		},
		AttributeDefinitions: []ddbtypes.AttributeDefinition{
			{AttributeName: aws.String("token"), AttributeType: ddbtypes.ScalarAttributeTypeS},
		},
	})
	if err != nil {
		t.Skipf("DynamoDB local not available: %v", err)
	}

	_, err = ddbClient.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   &sessionsTable,
		BillingMode: ddbtypes.BillingModePayPerRequest,
		KeySchema: []ddbtypes.KeySchemaElement{
			{AttributeName: aws.String("session_id"), KeyType: ddbtypes.KeyTypeHash},
		},
		AttributeDefinitions: []ddbtypes.AttributeDefinition{
			{AttributeName: aws.String("session_id"), AttributeType: ddbtypes.ScalarAttributeTypeS},
		},
	})
	if err != nil {
		t.Fatalf("failed to create sessions table: %v", err)
	}

	t.Cleanup(func() {
		ddbClient.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: &tokensTable})
		ddbClient.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: &sessionsTable})
	})

	return NewStoreWith(ddbClient, s3Client, tokensTable, sessionsTable, audioBucket)
}

// --- Token tests ---

func TestCreateTokenAndBurn(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	tok, err := store.CreateToken(ctx, "", "audio/test.mp3", "", "")
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if tok.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if tok.S3Key != "audio/test.mp3" {
		t.Fatalf("expected s3_key audio/test.mp3, got %s", tok.S3Key)
	}

	sess, err := store.BurnToken(ctx, tok.Token)
	if err != nil {
		t.Fatalf("BurnToken: %v", err)
	}
	if sess.SessionID == "" {
		t.Fatal("expected non-empty session_id")
	}
	if sess.Status != "active" {
		t.Fatalf("expected active status, got %s", sess.Status)
	}
	if sess.S3Key != "audio/test.mp3" {
		t.Fatalf("expected s3_key audio/test.mp3, got %s", sess.S3Key)
	}

	// Burn again — should fail (token already consumed)
	_, err = store.BurnToken(ctx, tok.Token)
	if err == nil {
		t.Fatal("expected error on second burn, got nil")
	}
}

func TestBurnNonexistentToken(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	_, err := store.BurnToken(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token")
	}
}

func TestCustomSlug(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	tok, err := store.CreateToken(ctx, "for-sarah", "audio/test.mp3", "", "")
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if tok.Token != "for-sarah" {
		t.Fatalf("expected token 'for-sarah', got %s", tok.Token)
	}
}

func TestSlugCollision(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	_, err := store.CreateToken(ctx, "my-song", "audio/a.mp3", "", "")
	if err != nil {
		t.Fatalf("first CreateToken: %v", err)
	}

	_, err = store.CreateToken(ctx, "my-song", "audio/b.mp3", "", "")
	if err == nil {
		t.Fatal("expected collision error, got nil")
	}
	if err.Error() != "token already taken" {
		t.Fatalf("expected 'token already taken', got %q", err.Error())
	}
}

// Upsert path: re-submitting the same token with the same s3_key updates the
// note and waveform in place. This is what the 2-phase frontend flow relies on
// (phase 1 reserves the slug, phase 2 adds the note).
func TestCreateTokenUpsertsOwnRow(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Phase 1: reserve "my-slug" with empty note and waveform
	_, err := store.CreateToken(ctx, "my-slug", "audio/xyz.mp3", "", "")
	if err != nil {
		t.Fatalf("phase 1 CreateToken: %v", err)
	}

	// Phase 2: same token + s3_key, now with note/waveform — must succeed
	_, err = store.CreateToken(ctx, "my-slug", "audio/xyz.mp3", "hello there", "10,20,30")
	if err != nil {
		t.Fatalf("phase 2 CreateToken (upsert): %v", err)
	}

	// Burn the token and check the session carries the updated note. If the
	// upsert didn't run, the session would see an empty note.
	sess, err := store.BurnToken(ctx, "my-slug")
	if err != nil {
		t.Fatalf("BurnToken: %v", err)
	}
	if sess.Note != "hello there" {
		t.Fatalf("expected note %q after upsert, got %q", "hello there", sess.Note)
	}
}

// Ownership check: a different caller using a different s3_key must NOT be
// able to upsert an existing token. This protects against slug squatting
// via the upsert path.
func TestCreateTokenUpsertRejectsDifferentS3Key(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	_, err := store.CreateToken(ctx, "shared-slug", "audio/owner.mp3", "", "")
	if err != nil {
		t.Fatalf("first CreateToken: %v", err)
	}

	// Attacker tries to overwrite note via upsert with a different s3_key
	_, err = store.CreateToken(ctx, "shared-slug", "audio/attacker.mp3", "pwned", "")
	if err == nil {
		t.Fatal("expected collision error with mismatched s3_key, got nil")
	}
	if err.Error() != "token already taken" {
		t.Fatalf("expected 'token already taken', got %q", err.Error())
	}
}

func TestEmptySlugGeneratesUUID(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	tok, err := store.CreateToken(ctx, "", "audio/test.mp3", "", "")
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if len(tok.Token) != 36 { // UUID format
		t.Fatalf("expected UUID-length token, got %q (%d chars)", tok.Token, len(tok.Token))
	}
}

func TestConcurrentBurnSameToken(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	tok, err := store.CreateToken(ctx, "", "audio/race.mp3", "", "")
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.BurnToken(ctx, tok.Token)
			if err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()

	if wins.Load() != 1 {
		t.Fatalf("expected exactly 1 winner in concurrent burn, got %d", wins.Load())
	}
}

// --- Note tests ---

func TestNotePropagatesToSession(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	tok, err := store.CreateToken(ctx, "", "audio/test.mp3", "listen to this", "")
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if tok.Note != "listen to this" {
		t.Fatalf("expected note 'listen to this', got %q", tok.Note)
	}

	sess, err := store.BurnToken(ctx, tok.Token)
	if err != nil {
		t.Fatalf("BurnToken: %v", err)
	}
	if sess.Note != "listen to this" {
		t.Fatalf("expected note 'listen to this' on session, got %q", sess.Note)
	}
}

func TestEmptyNoteOmitted(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	tok, err := store.CreateToken(ctx, "", "audio/test.mp3", "", "")
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	sess, err := store.BurnToken(ctx, tok.Token)
	if err != nil {
		t.Fatalf("BurnToken: %v", err)
	}
	if sess.Note != "" {
		t.Fatalf("expected empty note, got %q", sess.Note)
	}
}

func TestNotePreservedInGetSession(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	tok, _ := store.CreateToken(ctx, "", "audio/test.mp3", "remember this", "")
	sess, _ := store.BurnToken(ctx, tok.Token)

	got, err := store.GetSession(ctx, sess.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Note != "remember this" {
		t.Fatalf("expected note 'remember this', got %q", got.Note)
	}
}

// --- Session tests ---

func TestGetSession(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	tok, _ := store.CreateToken(ctx, "", "audio/test.mp3", "", "")
	sess, _ := store.BurnToken(ctx, tok.Token)

	got, err := store.GetSession(ctx, sess.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.SessionID != sess.SessionID {
		t.Fatalf("expected session %s, got %s", sess.SessionID, got.SessionID)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	_, err := store.GetSession(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestGetSessionAfterExpireStatus(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	tok, _ := store.CreateToken(ctx, "", "audio/test.mp3", "", "")
	sess, _ := store.BurnToken(ctx, tok.Token)

	// Directly mark status without S3 delete (no local S3)
	_, err := store.DDB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &store.SessionsTable,
		Key: map[string]ddbtypes.AttributeValue{
			"session_id": &ddbtypes.AttributeValueMemberS{Value: sess.SessionID},
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
		t.Fatalf("UpdateItem: %v", err)
	}

	// Should 404 now — indistinguishable from never-existed
	_, err = store.GetSession(ctx, sess.SessionID)
	if err == nil {
		t.Fatal("expected error for completed session")
	}
}

// --- Heartbeat tests ---

func TestHeartbeat(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	tok, _ := store.CreateToken(ctx, "", "audio/test.mp3", "", "")
	sess, _ := store.BurnToken(ctx, tok.Token)

	err := store.Heartbeat(ctx, sess.SessionID)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	got, err := store.GetSession(ctx, sess.SessionID)
	if err != nil {
		t.Fatalf("GetSession after heartbeat: %v", err)
	}
	if got.Status != "active" {
		t.Fatalf("expected active, got %s", got.Status)
	}
}

func TestHeartbeatOnCompletedSession(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	tok, _ := store.CreateToken(ctx, "", "audio/test.mp3", "", "")
	sess, _ := store.BurnToken(ctx, tok.Token)

	_ = store.ExpireSession(ctx, sess.SessionID, sess.S3Key)

	err := store.Heartbeat(ctx, sess.SessionID)
	if err == nil {
		t.Fatal("expected error on heartbeat for completed session")
	}
}

func TestHeartbeatOnNonexistentSession(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	err := store.Heartbeat(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error on heartbeat for nonexistent session")
	}
}

// --- Pause timeout tests ---

func TestSessionExpiredAfterPauseTimeout(t *testing.T) {
	store := setupTestStore(t)
	store.PauseTimeout = 1 // 1 second for test
	ctx := context.Background()

	tok, _ := store.CreateToken(ctx, "", "audio/test.mp3", "", "")
	sess, _ := store.BurnToken(ctx, tok.Token)

	time.Sleep(1100 * time.Millisecond)

	_, err := store.GetSession(ctx, sess.SessionID)
	if err == nil {
		t.Fatal("expected session to be expired after pause timeout")
	}
}

func TestHeartbeatKeepsSessionAlive(t *testing.T) {
	store := setupTestStore(t)
	store.PauseTimeout = 2
	ctx := context.Background()

	tok, _ := store.CreateToken(ctx, "", "audio/test.mp3", "", "")
	sess, _ := store.BurnToken(ctx, tok.Token)

	// Heartbeat at 1s, well within the 2s timeout
	time.Sleep(1 * time.Second)
	err := store.Heartbeat(ctx, sess.SessionID)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	// At 2s total — only 1s since last heartbeat, should still be alive
	time.Sleep(1 * time.Second)
	_, err = store.GetSession(ctx, sess.SessionID)
	if err != nil {
		t.Fatal("expected session to still be alive after heartbeat")
	}
}

// --- Complete / expire tests ---

func TestCompleteSessionDDBOnly(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	tok, _ := store.CreateToken(ctx, "", "audio/test.mp3", "", "")
	sess, _ := store.BurnToken(ctx, tok.Token)

	// Simulate complete by updating status directly (no local S3)
	_, err := store.DDB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &store.SessionsTable,
		Key: map[string]ddbtypes.AttributeValue{
			"session_id": &ddbtypes.AttributeValueMemberS{Value: sess.SessionID},
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
		t.Fatalf("UpdateItem: %v", err)
	}

	// Session gone
	_, err = store.GetSession(ctx, sess.SessionID)
	if err == nil {
		t.Fatal("expected 404 after complete")
	}

	// Heartbeat should fail
	err = store.Heartbeat(ctx, sess.SessionID)
	if err == nil {
		t.Fatal("expected heartbeat to fail after complete")
	}
}

func TestDoubleCompleteReturnsError(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	tok, _ := store.CreateToken(ctx, "", "audio/test.mp3", "", "")
	sess, _ := store.BurnToken(ctx, tok.Token)

	// Mark completed via DDB
	_, _ = store.DDB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &store.SessionsTable,
		Key: map[string]ddbtypes.AttributeValue{
			"session_id": &ddbtypes.AttributeValueMemberS{Value: sess.SessionID},
		},
		UpdateExpression: aws.String("SET #s = :done"),
		ExpressionAttributeNames: map[string]string{
			"#s": "status",
		},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":done": &ddbtypes.AttributeValueMemberS{Value: "completed"},
		},
	})

	// CompleteSession should fail — GetSession returns error for non-active
	err := store.CompleteSession(ctx, sess.SessionID)
	if err == nil {
		t.Fatal("expected error on double complete")
	}
}
