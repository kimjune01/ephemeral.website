# ephemeral.website

One listen. Then it's in their head.

Upload or record audio, name a link, share it. The recipient gets one listen — forward-only, no rewind, no replay. Pause triggers a 15-second countdown. When it hits zero, the audio is gone. After playback completes, the URL 404s. The S3 object is deleted. The bits are gone.

## Stack

Go everywhere. Lambda functions, API Gateway, DynamoDB, S3 pre-signed URLs, Route53. No frameworks, no CloudFront. Frontend is vanilla HTML/CSS/JS embedded in a Go binary.

## Deploy

```bash
cd backend && bash build.sh   # copies frontend/ → embedded static, compiles Lambdas
cd ../infra && pulumi up      # deploys all Lambdas
```

Edit `frontend/` — `build.sh` syncs it into the Go binary automatically.

## Dev

```bash
docker run -d -p 8000:8000 amazon/dynamodb-local
cd backend && go test ./... -v
```
