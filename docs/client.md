# ephemeral.website client contract (v1)

The [API docs](api.md) describe the HTTP wire contract. This document describes
the client-side protocols built on top of it that make the one-listen primitive
actually feel instant and never lose the user's work.

Two patterns carry most of the weight:

1. **Two-phase link creation** (sender side) — reserve the slug early so Send
   reveals the share page without waiting.
2. **Two-phase consumption** (listener side) — preview safely before the
   commit, burn the token only after real playback has happened.

Everything else is UX polish, iOS workarounds, or small touches that compound.

---

## 1. Two-phase link creation

Goal: the share page reveals instantly when the user taps Send, and a slug
collision never costs the user their recording.

### Phase 0 — Kickoff (runs in parallel with typing)

The moment the user selects a file or finishes recording, fire two background
workers without waiting for the user to do anything:

```
POST /api/upload { content_type }
  → returns { token, upload_url, s3_key }

PUT  <upload_url>  (audio bytes)
  → S3 ingests the audio while the user is still typing their slug
```

The initial token is thrown away — you only need the `s3_key` and the
in-flight S3 PUT. The audio is already uploading before the user has thought
about what to name it. A 5MB file takes 1–3 seconds over 4G; hiding that
behind typing is where most of the perceived speed comes from.

Reference: `frontend/ephemeral.js` → `kickoffOptimistic()`.

At the same time, start computing the waveform off the file in parallel
(`computeWaveform()`) so it's ready by Send time.

### Phase 1 — Reserve (on slug blur / note focus)

When the user commits to a slug — the `blur` event on the slug input OR
the `focus` event on the note input, whichever fires first — re-POST with
the slug and the cached s3_key:

```
POST /api/upload { slug, content_type, s3_key }
  → returns { token: slug }  (creates the row atomically)
  → OR 409 if the slug is already taken
```

If this returns 409, show the "slug taken" error **inline on the note form**.
The user's audio is still uploaded, their note is still in the textarea, and
they can pick a different slug without losing anything. This is the rule:
the user never starts over.

Reference: `frontend/ephemeral.js` → `reserveSlug()`, wired to both
`slugInput.blur` and `noteInput.focus`.

### Phase 2 — Commit (on Send)

When the user taps Send, the token already exists from phase 1. Compute the
final URL synchronously and reveal the share page **before any network call
fires**:

```
linkOutput.value = `${origin}/${reservedToken.token}`;
reveal(result);  // share page visible immediately
```

Then, in the background, upsert the note and waveform:

```
POST /api/upload { slug, note, waveform, content_type, s3_key }
  → server matches s3_key to the existing row and updates note + waveform
  → same token returned, no collision
```

The `s3_key` is the ownership primitive. Only the original uploader holds it
(the server returned it once, on the phase 0 create). The server's upsert
logic: if the slug already exists AND the submitted s3_key matches the
existing row's s3_key, update note + waveform in place. Otherwise, 409.

Reference: `frontend/ephemeral.js` → `upload()` → the background IIFE after
`reveal(result)`.

### Why two phases?

Because phase 1 creates the row, the client already knows the final URL by
phase 2. Phase 2's POST is fire-and-forget from the UX perspective — the user
has already copied the link before it returns. A typical Send takes ~10ms
from tap to share page, not 500ms–2s.

And because phase 1 is the only place a slug collision can happen, the user
never sees the share page bounce back to the form. If phase 1 failed with a
409, the user is still on the slug form and sees the inline error.

---

## 2. Two-phase consumption

Goal: the token is only burned after the listener has actually heard the
audio — no bot visit, no preview, no accidental tap can destroy the whisper.

### Phase 0 — Pre-check (on page load, no commitment)

Before rendering any playable UI, verify the token exists:

```
GET /api/check/{token}
  → { exists: "true", stream_url, note?, waveform? }
  → OR { exists: "false" }
```

If the token doesn't exist, show the "gone" state immediately. Don't make
the user tap a play button only to discover the link is dead.

The `/check` response includes a short-lived presigned S3 stream URL you can
use to preload the audio data (`audio.preload = 'auto'`) before the user
taps anything. This makes the actual playback start instantaneously.

Critically, `/check` does **not** burn the token. It's safe to call as many
times as you want. A thousand previews and one listen still leaves exactly
one listen's worth of state change.

Reference: `frontend/ephemeral.js` → IIFE at top of `initPlayer()`.

### Phase 1 — Prime (on tap, during the 3-second countdown)

When the listener taps the play button, do NOT burn the token yet. Instead:

1. Start a 3-second animated fill on the play pill — this is the listener's
   "wait, I'm not ready" escape hatch. Tapping again cancels.
2. In the same tap handler, **prime** the `<audio>` element for iOS by
   creating it with `volume = 0`, calling `.play()` synchronously inside the
   gesture, and pausing as soon as the play promise resolves. This unlocks
   the element for later use without any audible blip.
3. Load the preloaded stream URL into the element.

The token is still not burned. If the user cancels within 3 seconds, no
state has changed.

Reference: `frontend/ephemeral.js` → `playBtn.addEventListener('click', …)`
→ the "Prime audio in the user-gesture context" block.

### Phase 2 — Commit (after real playback begins)

When the 3-second countdown completes, call `audio.play()` for real (it's
already unlocked from phase 1). Then monitor `audio.currentTime` in a
`requestAnimationFrame` loop.

When `currentTime >= 1` (one full second of confirmed playback), THEN burn
the token:

```
POST /api/session { token }
  → returns { session_id, note?, pause_timeout }
```

Start sending heartbeats every 5 seconds:

```
POST /api/heartbeat/{session_id}
  → 200 (keep going)
  → 4xx (server gave up, navigate to gone state)
```

When the `<audio>` fires `ended`, complete the session and delete the S3
object:

```
POST /api/complete/{session_id}
  → server deletes the audio bytes
```

Reference: `frontend/ephemeral.js` → `startPlaying()` → inside
`tickPlayback()`, the `if (audio.currentTime >= 1 && !sessionId)` block.

### Why two phases?

Because the burn happens at 1 second of confirmed playback instead of at tap
time, three classes of accidents no longer consume whispers:

1. A bot visiting the URL (fetches /check, never taps play).
2. A listener who tapped play by accident and cancels within 3 seconds.
3. A browser that blocks autoplay and never actually starts playing.

In all three cases, the whisper survives and remains available for the real
listener. Without this split, the one-listen rule becomes a one-tap rule,
and taps are cheap.

### Pause semantics

When the listener pauses during playback, show a visible 15-second countdown
(this matches the server's `pause_timeout`). If they resume before it
expires, cancel the countdown and continue sending heartbeats. If they don't,
send complete and navigate to gone.

The countdown isn't just visual — the server really will expire the session
if no heartbeats arrive within 15 seconds. The client must honor the same
deadline for the UI to stay in sync.

Reference: `frontend/ephemeral.js` → `startCountdown()`, `updateCountdownDisplay()`.

### Heartbeat failure is NOT fatal

A single failed heartbeat (network hiccup, DNS blip, backgrounded tab) must
not navigate to gone. Only an explicit HTTP error response should do that.
The existing implementation swallows network-level errors on heartbeat and
only calls `gone()` on `!resp.ok` (which is the server saying "your session
is dead").

Reference: `frontend/ephemeral.js` → `sendHeartbeat()`.

---

## 3. UX optimizations

Everything else that isn't strictly required but makes the product feel like
a product. Each item is small; the pile of them is what distinguishes a
working prototype from something you'd actually use.

### Sender-side

- **Required, non-empty slug.** The Send button stays disabled until the
  slug field has content, even though the backend would happily generate a
  UUID. The act of naming the whisper is part of the experience. No slug,
  no send.

- **Live slug normalization.** On every `input` event, normalize the slug
  in place: lowercase, spaces → hyphens, strip non-alphanum-hyphen, collapse
  runs of hyphens. The user can type freely and the value is always valid.
  No "that name has an invalid character" error ever shows.

- **Optimistic waveform computation.** Decode the audio file into an
  `AudioBuffer` as soon as it's selected, sample 100 RMS peaks, normalize
  0–100, send as a comma-separated string in the phase-2 POST. The server
  stores it opaquely and returns it on `/check`. The listener's player
  renders it as an SVG fill behind the pause pill.

- **Tab-content layout-shift lock.** The record tab and upload tab each
  have `min-height: 220px` and flex-center their content. Switching tabs
  does not shift other elements on the page. Any two-child layout where the
  children have different intrinsic heights will have this problem; the fix
  is to lock the container.

- **Slug reservation invalidates on slug edit.** If the user changes the
  slug after a reservation has been issued, clear `reservedToken` and
  `reservePromise` so the next blur/focus re-reserves. Otherwise stale
  reservations leak into Send.

- **Two-line upload prompt.** "Upload audio" on one line, "up to 5MB" as a
  smaller uppercase sub-line. The golden-ratio box (17rem × ~10.5rem)
  balances around the centered text. Small detail, noticeably better than
  a single line of run-together copy.

- **"so i made this website" footer tone.** Small, lowercase, no period.
  Signals that this is a handmade website, not a product, and pre-empts
  every product-shaped question.

### Recording

- **2-minute hard cap.** `mediaRecorder.stop()` fires automatically when
  `recordSeconds >= 120`. The cap is a content filter: if you need more
  than 2 minutes, this is the wrong product.

- **Urgency timer scaling.** The recording countup text scales from 1x to
  3x via CSS `transform` over the last 60 seconds (starts at the halfway
  point, peaks at the cap). Urgency signal without a modal or a beep.

- **Volume indicator glow.** An `AudioContext` analyser reads mic level
  each frame and pulses a red `box-shadow` around the record button. This
  catches "mic permission granted but capturing silence" — the user sees
  no glow and knows something's wrong before they finish recording.

- **Copy-icon swap on button press.** After clicking Copy, the chain link
  icon swaps to a checkmark SVG for 2 seconds, then restores. Uses the
  cached original innerHTML, no text swapping (which would break the
  icon).

- **Web Share API fallback.** The Share button is hidden by default and
  only revealed if `navigator.share` exists. Uses the native share sheet
  on mobile; desktop users just see the Copy button.

### Listener-side

- **Pre-check token before rendering the player.** Avoids showing a play
  button for a dead link. Also returns a preloadable stream URL so the
  actual playback starts with zero buffering delay after the 3s countdown.

- **3-second countdown with cancel.** A filled pill fades in over 3 seconds.
  Tapping again cancels. Token is not burned until 1s after the countdown
  completes and real playback begins.

- **Note displayed during countdown.** The sender's note fades in while the
  play pill is filling, giving the listener context before the audio starts.
  Fades out when playback begins so it doesn't compete with the audio.

- **Breathing background animation.** `@keyframes breathe` gently shifts
  the body `background-color` between `#0a0a0a` and `#0f0f0f` on a 4-second
  loop during active playback. Paused via `animation-play-state` during
  pause state. Subtle but it makes the page feel alive with you.

- **Forward-only player.** No scrub bar, no rewind, no skip. The progress
  bar is a read-only SVG fill. Giving the listener control turns a whisper
  into a recording.

- **Haptic vibrate on completion.** `navigator.vibrate(200)` on completed
  playback. Small physical confirmation that the whisper landed.

- **"Record one back" primary CTA.** On completed playback, the gone-state
  CTA switches from an outlined "Make your own" to a filled "record one
  back →" with a subtle cyan/magenta glow. Turns the exit into an
  invitation.

- **Chromatic aberration on the wordmark.** Cyan/magenta text-shadow offset
  with a slow drift animation. Aesthetic commitment that signals "handmade
  website, not SaaS product" from the first paint.

### Mobile (iOS + Android)

- **`100dvh` viewport with safe-area insets.** `min-height: 100dvh` with
  `100vh` as fallback; `padding: env(safe-area-inset-*)` on body so
  content avoids the notch and home indicator. Also `viewport-fit=cover`
  in the meta tag.

- **`overflow-x: hidden` on body.** Kills any horizontal scroll glitches
  from long URLs or misaligned children. Cheap, catches a whole class of
  mobile-only bugs.

- **44px minimum tap target on buttons.** `min-height: 44px` and flex
  center alignment on the Copy/Share buttons. Matches iOS HIG.

- **Narrow-phone breakpoint at 360px.** iPhone SE and small Androids get
  tighter padding, smaller h1, smaller subtitle. Avoids layout crowding on
  the smallest real screens.

- **Landscape breakpoint.** `@media (max-height: 480px) and (orientation:
  landscape)` anchors content to the top instead of centering — vertical
  space is scarce in landscape and flex-centering wastes it.

### iOS Safari quirks (all required)

These are not optional. Skipping any of them silently breaks recording or
playback without any console error.

- **`audio.play()` must be in a direct user gesture.** iOS considers async
  continuations, rAF callbacks, and setTimeout callbacks to all be outside
  the gesture. Prime the audio element at volume 0 inside the tap handler,
  then pause immediately when the play promise resolves. Later, the element
  is unlocked for playback from anywhere.

- **MediaRecorder doesn't support `audio/webm`.** iOS emits `audio/mp4`.
  Probe with `MediaRecorder.isTypeSupported()` and use whatever comes back.
  Store the actual MIME and use it consistently for the Blob, the File, and
  the `content_type` sent to the API.

- **AudioContext starts suspended.** After `new AudioContext()`, call
  `if (audioCtx.state === 'suspended') await audioCtx.resume()` inside the
  user gesture that created it. Required for both the recording volume
  analyser and the waveform decode.

- **File picker `accept="audio/*"` is unreliable.** iOS Files app doesn't
  always tag downloaded files with `audio/*`. Broaden to
  `accept="audio/*,.mp3,.m4a,.wav,.aac,.ogg,.flac,.opus,.webm,.mp4"` and
  validate with both MIME type AND filename extension.

- **Input `font-size: 16px` on mobile.** If a focused input has a computed
  `font-size < 16px`, iOS zooms in and won't zoom back out. Every input/
  textarea needs `font-size: 16px` on the mobile media query, with
  specificity high enough to beat any class-level overrides.

### Cache control during iteration

- **Short max-age while iterating, long max-age in steady state.** During
  active development, CSS/JS `Cache-Control: public, max-age=60` so changes
  show up on refresh within a minute. After things stabilize, bump to
  `max-age=3600` or higher. HTML stays `no-cache` always.

---

## 4. Fallback paths

Every optimization has a fallback. When the happy path fails, don't crash,
don't lose work, and don't surprise the user.

- **Optimistic upload failure → `blockingUpload()`.** If phase 0 or phase 1
  fails entirely, `reservedToken` remains null. The `upload()` function
  detects this and calls `blockingUpload()`, which runs the traditional
  sequential flow: POST on Send, wait for response, PUT to S3, show share
  page. Slower, but always works.

- **Phase-1 slug collision → inline error, stay on note form.** A 409
  during phase 1 sets `slugError.hidden = false`, focuses the slug input,
  and does not navigate. The user's audio is still uploaded; they pick a
  different slug and phase 1 retries.

- **Phase-2 network error → silent.** The share page is already revealed.
  If the phase-2 POST fails, the token still exists (from phase 1) and the
  share URL still works. The note and waveform just aren't attached. Prefer
  this to interrupting a user who has already copied the link.

- **Heartbeat network error → retry next interval.** Do not navigate to
  gone. Only a clear HTTP error response ends the session.

- **S3 PUT failure in phase 0 → re-upload on Send.** `canReuseS3Key` is
  tracked separately. If the background S3 PUT rejected, phase 2 requests
  a fresh upload URL and re-PUTs synchronously. The user never sees the
  first failure.

- **MediaRecorder unsupported MIME → try the next one.** If neither
  `audio/webm` nor `audio/mp4` is supported, fall back to `new
  MediaRecorder(stream)` with no mimeType and use whatever the browser
  defaults to. Store the actual value on the instance.

- **`crypto.randomUUID` not available.** Not currently used since slugs are
  required, but any client relying on UUIDs as tokens should fall back to
  `crypto.getRandomValues` or a simple hex-from-entropy helper for Safari
  versions before 15.4.

---

## 5. Minimum viable client

If you want to strip this down to the bare minimum and still honor the
one-listen primitive, here's what's required vs. optional.

### Required

- `POST /api/upload` to create a token and get an upload_url
- `PUT` audio bytes to the upload_url
- For listening: `GET /api/check/{token}`, `POST /api/session`, `GET
  /api/stream/{session_id}`, `POST /api/heartbeat` every 5s, `POST
  /api/complete` on `ended`
- On iOS: prime the `<audio>` element at volume 0 inside a user gesture
- On iOS: probe MediaRecorder MIME type
- On iOS: resume AudioContext after create

### Strongly recommended (single-listen guarantees)

- Pre-check the token with `/check` before showing play UI
- Burn the token only after 1 second of confirmed playback
- 3-second countdown before real playback with a cancel option
- Visible 15-second pause countdown that matches the server timeout
- Tolerate heartbeat network errors; only navigate on explicit HTTP errors

### Optional (UX polish)

- Two-phase reservation for instant Send
- Client-computed waveform rendering
- Forward-only player (no scrub bar)
- Slug normalization on input
- Hard recording time cap with scaling urgency timer
- Volume indicator during recording
- Haptic vibrate on completion
- "Record one back" CTA after completed play
- Chromatic aberration on the wordmark

### Layer-specific (send-only, like confession.website)

A layer that only creates whispers and hands off links needs only the
sender-side pieces:

- `POST /api/upload` and S3 PUT
- iOS MediaRecorder MIME detection
- Recording time cap and size validation
- Copy/Share buttons for the result

The playback side isn't the layer's problem — the recipient clicks the
shared link and lands on ephemeral.website, which runs the full
listener-side protocol described above. The layer only needs to honor the
sender-side constraints.
