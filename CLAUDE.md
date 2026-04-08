# ephemeral.website

Ephemeral audio playback. Upload audio, get a one-time link. One listen, then 404.

## Stack

- **Infra**: Pulumi (Go) in `infra/`
- **Backend**: Go Lambda functions in `backend/`
- **Frontend**: Vanilla HTML/CSS/JS in `frontend/`
- **Storage**: S3 (audio), DynamoDB (tokens + sessions)

## Build & deploy

```bash
cd backend && bash build.sh   # copies frontend/ → embedded static, compiles Lambdas
cd ../infra && pulumi up      # deploys all Lambdas
```

Edit `frontend/` — `build.sh` syncs it into `backend/cmd/site/static/` automatically. Never edit the static copy directly.

## Backend structure

- `backend/internal/` — shared store (DynamoDB + S3 operations)
- `backend/cmd/{upload,session,stream,heartbeat,complete}/` — Lambda handlers
- `backend/build.sh` — cross-compiles to linux/arm64, zips for Lambda

## API endpoints

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/upload` | Accept audio, store in S3, return token |
| POST | `/session` | Burn token, create playback session |
| GET | `/stream/{session_id}` | Stream audio with range requests |
| POST | `/heartbeat/{session_id}` | Reset pause timer |
| POST | `/complete/{session_id}` | Mark done, delete audio |

## Key behaviors

- Token burn is atomic (DynamoDB conditional delete)
- Pause timeout: 15 seconds. Server checks lazily on each request.
- Player: forward-only, no rewind, no seek. Read-only progress bar.
- After playback or timeout: S3 object deleted, all endpoints 404.
- 404 is indistinguishable from "never existed."

## Tests

```bash
# Unit tests — require DynamoDB local on :8000
docker run -d -p 8000:8000 amazon/dynamodb-local
cd backend && go test ./internal/ -v

# End-to-end smoke test against live infra — run after every deploy
bash scripts/smoke-test.sh
```

The smoke test hits https://ephemeral.website/api and walks the full
round trip: upload → check → session burn → stream → heartbeat → complete,
plus the 2-phase reserve + upsert flow and a real slug collision. Needs
`curl` and `jq`. Override the API base with `API=... bash scripts/smoke-test.sh`.
