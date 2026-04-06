# ephemeral.website

Upload audio. Share a link. One listen, then it's gone.

## How it works

1. Upload an audio file (max 5 MB)
2. Get a shareable link
3. Recipient clicks the link and hears the audio once
4. Pause is allowed — but a 15-second countdown starts. If it hits zero, the audio is gone.
5. No rewind. No replay. After playback completes, the URL returns 404.

## Deploy

```bash
cd backend && bash build.sh
cd ../infra && pulumi up
```

## Dev

Backend tests require DynamoDB Local:

```bash
docker run -d -p 8000:8000 amazon/dynamodb-local
cd backend && go test ./... -v
```
