# ephemeral.website public API (v1)

Base URL: `https://ephemeral.website/api`

This API is public and open — no keys, no auth. It is intended to be built
upon. If you are building a product on top of ephemeral (a "layer"), you
should only need the routes below.

**See also:** [`client.md`](client.md) — the client-side protocols that make
this API actually work correctly. It documents the two-phase link creation
flow, the two-phase consumption flow, all the UX optimizations, iOS Safari
workarounds, and fallback paths. If you're implementing a listener or a
sender from scratch, you want that document as much as this one.

The primitive: upload an audio file, get back a one-time playback link. When the
recipient plays the link once, the audio is deleted from storage and every
endpoint referring to the token returns 404. There is no way to "undelete" a
whisper. 404 is indistinguishable from "never existed."

## Stability

These routes are considered v1 and will not change in a breaking way. New fields
may be added to response bodies; existing fields will not be removed or renamed.
Any breaking changes will be introduced under `/api/v2/*` with a deprecation
period on v1.

## Limits

- Audio files must be ≤ 5 MB.
- Audio `content_type` must start with `audio/`.
- Slugs must match `^[a-z0-9-]{1,64}$`. Slug can be omitted, in which case the
  server generates a UUID.
- Notes are capped at 280 characters (server-truncates longer input).
- Tokens expire from storage after 7 days whether or not they have been played.
- Paused playback resumes after a 15-second inactivity timeout expires the session.

## CORS

CORS is restricted to the `.website` family of first-party layers. The current
allowlist:

- `https://ephemeral.website`
- `https://appreciation.website`
- `https://confession.website`

Allowed methods: `GET`, `POST`, `OPTIONS`. Allowed headers: `Content-Type`.
Preflight is handled at the API Gateway level, no manual OPTIONS handling
required. If you are building a new layer and want cross-origin access from
the browser, open an issue or PR to add your origin to the allowlist. Server-
to-server calls are not subject to CORS and work from anywhere.

---

## POST `/api/upload`

Create a new whisper. Returns a token identifying the whisper, a link to share,
a pre-signed URL for uploading the audio bytes to S3 directly, and the S3 key
used for storage.

### Request

```json
{
  "slug": "optional-custom-slug",
  "note": "optional short note (max 280 chars)",
  "waveform": "optional comma-separated peaks (0-100)",
  "content_type": "audio/mpeg",
  "s3_key": "audio/<uuid>  (optional — see below)"
}
```

- `content_type` is **required** and must start with `audio/`.
- `slug` is optional. If provided, the whisper is reachable at
  `ephemeral.website/<slug>` and you cannot use a slug that is already taken
  (unless you also supply the `s3_key` of the existing whisper — see below).
  If omitted, the server generates a UUID.
- `note` is optional and will be displayed to the recipient during the pre-play
  countdown.
- `waveform` is optional and will be rendered in the player as a visualization.
  Comma-separated integers from 0 to 100, usually 100 values.
- `s3_key` is optional. If supplied, it must match `audio/<uuid>` format and the
  new whisper will reuse the referenced S3 object instead of receiving a fresh
  pre-signed URL. This enables 2-phase flows: upload once, reference many times.
  See "Upsert semantics" below.

### Response (`201 Created`)

```json
{
  "token": "my-slug-or-uuid",
  "url": "/my-slug-or-uuid",
  "upload_url": "https://s3.amazonaws.com/... (presigned PUT)",
  "s3_key": "audio/<uuid>"
}
```

- `token` is the identifier for the whisper. Use it to construct the share URL:
  `https://ephemeral.website/${token}`.
- `upload_url` is a pre-signed S3 PUT URL valid for a short period. Upload the
  audio bytes directly to this URL with `Content-Type` matching what you sent.
- `s3_key` can be passed back on subsequent `POST /api/upload` calls to reference
  the same S3 object (see "Upsert semantics").

### Error responses

- `400` — Invalid JSON, missing or non-audio `content_type`, invalid slug format,
  invalid `s3_key` format.
- `409` — Slug already taken (and supplied `s3_key` does not match the existing
  whisper's s3_key).
- `500` — Store or S3 presign error.

### Upsert semantics

If you `POST /api/upload` with a `slug` that already exists, the server checks
the `s3_key` field:

- If your `s3_key` matches the existing whisper's `s3_key`, the server updates
  the existing whisper's `note` and `waveform` in place and returns `201`.
  This lets you "reserve" a slug in phase 1 and update it with the final note
  in phase 2, without losing the slug.
- If your `s3_key` does not match (or you didn't supply one), the server returns
  `409 Conflict` and does not modify the existing whisper.

This is how you should authenticate writes: only the original creator of a
whisper holds its `s3_key`, because the server returned it only once on the
initial create.

### Example

```bash
curl -X POST https://ephemeral.website/api/upload \
  -H 'Content-Type: application/json' \
  -d '{"slug":"just-for-you","content_type":"audio/mpeg"}'
# => { "token": "just-for-you", "upload_url": "https://...", "s3_key": "audio/..." }

# Upload the bytes:
curl -X PUT "$UPLOAD_URL" \
  -H 'Content-Type: audio/mpeg' \
  --data-binary @voice-memo.mp3
```

---

## GET `/api/check/{token}`

Check whether a token still exists and, if so, return its metadata and a short-
lived stream URL for preloading. This is safe to call repeatedly — it does not
burn the token.

### Response (`200 OK`)

If the token exists:

```json
{
  "exists": "true",
  "note": "optional note attached to whisper",
  "waveform": "10,20,30,...",
  "stream_url": "https://s3... (presigned GET, short-lived)"
}
```

If the token does not exist (404, consumed, expired):

```json
{ "exists": "false" }
```

Note: `exists` is returned as a string, not a boolean, for JSON compatibility
with clients that distinguish the two.

### Example

```bash
curl https://ephemeral.website/api/check/just-for-you
# => { "exists": "true", "stream_url": "https://..." }
```

---

## POST `/api/session`

Burn a token and open a playback session. This is the atomic "consume" step:
after this succeeds, the token is gone and the audio can only be played through
the returned `session_id`. Calling this twice on the same token returns 404 on
the second call.

### Request

```json
{ "token": "just-for-you" }
```

### Response (`201 Created`)

```json
{
  "session_id": "<uuid>",
  "pause_timeout": 15,
  "note": "optional note"
}
```

- `session_id` is the identifier you use for the `/stream`, `/heartbeat`, and
  `/complete` endpoints.
- `pause_timeout` is the number of seconds the server will wait between
  heartbeats before expiring the session.
- `note` is included if the original whisper had one.

### Error responses

- `404` — Token does not exist, was already burned, or expired.

---

## GET `/api/stream/{session_id}`

Stream the audio bytes for an active session. Returns the raw audio with an
appropriate `Content-Type` header. Supports HTTP range requests for partial
playback.

The stream endpoint is CORS-open, so you can embed the audio directly in an
`<audio>` element on any origin:

```html
<audio src="https://ephemeral.website/api/stream/abc123" controls></audio>
```

If the session has expired (timeout or completed), returns 404.

---

## POST `/api/heartbeat/{session_id}`

Reset the pause timer for a session. Call this periodically (every ~5s is
typical) while audio is actively playing. If the server does not hear from you
within `pause_timeout` seconds, the session expires and subsequent requests
return 404.

### Response

- `200 OK` — Session is still alive.
- `404` — Session does not exist, has expired, or has been completed.

---

## POST `/api/complete/{session_id}`

Mark a session as completed. This tells the server you are done with playback.
The server deletes the audio object from S3 and marks the session as finalized.
Subsequent requests against this session return 404.

You should call this when audio playback ends naturally (the `ended` event on
the `<audio>` element) or when the user explicitly cancels.

### Response

- `200 OK` — Session marked complete, audio deleted.
- `404` — Session does not exist.

---

## Typical flows

### Create and share a whisper

```
1. POST /api/upload           { slug, content_type }  →  { token, upload_url, s3_key }
2. PUT  upload_url            (audio bytes)           →  200
3. Share https://ephemeral.website/<token>
```

### Build a layer that sends to a handle

Layers (e.g. appreciation.website, confession.website) can delegate all audio
storage to ephemeral and only track their own handle → token mapping:

```
On upload:
1. POST /api/upload           { slug, content_type } on ephemeral.website
2. PUT  upload_url            (audio bytes) on S3
3. Save (recipient_handle, token) in layer's own DB

On inbox view:
4. GET /api/check/{token}     → confirm whisper still exists
5. Redirect user to https://ephemeral.website/{token} for playback
   (OR embed the core player if preferred)
```

Layers never touch audio bytes after the upload. The core's one-listen rule is
enforced regardless of how many layers reference it.

### 2-phase reservation for instant share UX

```
On file select:
1. POST /api/upload           { content_type } → { token0, upload_url, s3_key }
2. PUT  upload_url            (start audio upload in background)

On slug blur (phase 1 reserve):
3. POST /api/upload           { slug: user_slug, content_type, s3_key } → { token: user_slug }

On Send (phase 2 upsert):
4. POST /api/upload           { slug: user_slug, note, waveform, content_type, s3_key }
   → upserts note+waveform on the same row (server matches s3_key to authenticate)
5. Show share page immediately — no waiting
```
