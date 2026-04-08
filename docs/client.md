# Building a layer on top of ephemeral.website

This document exists to hasten the development of new layers
(confession.website, appreciation.website, and whatever comes next). If you
are building a themed front door for the one-listen voice primitive, start
here. If you are trying to understand how ephemeral.website's own frontend
works, [the reference section at the bottom](#reference-the-core-clients-full-protocol)
will also help — but that's a secondary use.

The [API docs](api.md) describe the HTTP wire contract. This document
describes which client-side patterns you should copy, which you should leave
to the core, and the iOS workarounds you cannot skip.

---

## What a layer is (and isn't)

A layer is a themed front door for the same voice primitive that
ephemeral.website already exposes. The layer's job:

- **Record or accept audio** from the user
- **Create a whisper** via `POST /api/upload` on ephemeral.website
- **Upload the audio bytes** via the returned presigned S3 URL
- **Hand off the share URL** (`https://ephemeral.website/<token>`) to the
  sender for them to distribute

The layer's job explicitly does NOT include:

- Playback. The recipient clicks the shared link and lands on
  ephemeral.website, which runs the full listener protocol. Your layer's
  URL is a send-only origin.
- Session management, heartbeats, completion marking, or the 15-second
  pause timeout. None of that runs on the layer's origin.
- Waveform rendering (unless you want to, but it only shows during
  playback on ephemeral.website's origin, so computing it on the layer is
  decorative).
- Any state storage beyond what the browser already provides (no server-
  side database, no accounts, no DynamoDB).

The layer contains roughly 200 lines of JS, 300 lines of CSS, and one
index.html. Both confession.website and appreciation.website fit this
description exactly. They share zero code but they're built by copy-paste
from a common starting point.

---

## Quickstart: copy an existing layer

The fastest path to a new layer is to clone an existing scaffold and
rebrand. Both repos live as siblings of ephemeral.website:

```
~/Documents/confession.website
~/Documents/appreciation.website
```

To start a new layer:

1. Copy one of the two directories to a new sibling: `cp -r
   ~/Documents/confession.website ~/Documents/yourthing.website`
2. Delete `.git/` and reinit: `cd yourthing.website && rm -rf .git && git
   init`
3. Edit `frontend/index.html`: change the `<title>`, `<h1>`, subtitle copy,
   and button labels. The DOM structure can stay identical.
4. Edit `frontend/style.css`: change the color palette, the chromatic
   aberration hue, fonts if you want. The layout can stay identical.
5. Edit `frontend/script.js`: change any text labels that live in JS
   (share title, share text). The recording and upload flow stays
   identical.
6. Edit `README.md` and `CLAUDE.md` with the new concept and tone notes.
7. Add your new origin to `infra/main.go` in the ephemeral.website repo,
   in the `CorsConfiguration.AllowOrigins` list, and redeploy. Without
   this, CORS preflight will fail and your layer's `POST /api/upload`
   calls will be blocked from the browser.

That's the entire setup. You can preview locally with `python3 -m
http.server 12345 --directory frontend/` (API calls will fail from
localhost unless you also add `http://localhost:12345` to the CORS
allowlist), or deploy the three files as a static site on whatever host
you prefer.

---

## What you must copy from the existing scaffolds

These are the load-bearing patterns. Skip any of them and the layer will
break on iOS, lose audio on errors, or feel sluggish. They are already
implemented in the two scaffolds — you just need to not break them when
you rebrand.

### 1. Detect the MediaRecorder MIME type

```js
const recMime = MediaRecorder.isTypeSupported('audio/webm') ? 'audio/webm'
              : MediaRecorder.isTypeSupported('audio/mp4')  ? 'audio/mp4'
              : '';
mediaRecorder = recMime
    ? new MediaRecorder(stream, { mimeType: recMime })
    : new MediaRecorder(stream);
recordedMime = mediaRecorder.mimeType || recMime || 'audio/webm';
```

iOS Safari's MediaRecorder does NOT support `audio/webm`. It only supports
`audio/mp4`. If you hardcode webm, recordings on iPhone will be broken
files that the server will accept and the recipient will be unable to
play. Use the detected MIME type consistently for the Blob, the File, and
the `content_type` in the upload request.

### 2. Hard cap recording at 2 minutes with scaling timer urgency

```js
const MAX_RECORD_SECONDS = 120;
// ...
recordTimer = setInterval(() => {
    recordSeconds++;
    // update display
    const half = MAX_RECORD_SECONDS / 2;
    const t = Math.max(0, (recordSeconds - half) / half);
    const scale = 1 + 2 * Math.min(t, 1);
    recordTime.style.transform = `scale(${scale})`;

    if (recordSeconds >= MAX_RECORD_SECONDS && mediaRecorder && mediaRecorder.state === 'recording') {
        mediaRecorder.stop();
    }
}, 1000);
```

The cap is a content filter, not just a technical limit: if the user
needs more than 2 minutes, this is the wrong product. The timer scales
from 1x to 3x in the last 60 seconds via CSS transform. Urgency signal
without a modal.

Match the ephemeral.website value (120s) so senders who use multiple
layers have a consistent experience.

### 3. Validate size on the client before uploading

```js
if (recordedBlob.size > 5 * 1024 * 1024) {
    alert('too long — must be under 5MB.');
    resetRecorder();
    return;
}
```

5 MB is the hard server limit. Catching it on the client with a clear
message is better than letting the S3 PUT fail halfway through.

### 4. Cross-origin API calls go to the absolute URL

```js
const API = 'https://ephemeral.website/api';
// ...
await fetch(`${API}/upload`, { ... });
await fetch(data.upload_url, { method: 'PUT', ... });
```

Never use a relative `/api` path on a layer — the layer's own origin
doesn't host the API. Always the absolute `https://ephemeral.website/api`.
And your origin must be in the ephemeral CORS allowlist.

### 5. Hand off the playback URL to ephemeral.website

```js
linkOut.value = `https://ephemeral.website/${data.token}`;
```

Not `https://yourlayer.website/${data.token}`. The playback page doesn't
exist on your layer's origin — the core's player handles listening.
Generating the share URL against ephemeral.website is deliberate: the
sender copies a ephemeral.website URL, and the recipient never visits
your layer for playback. Your layer is a send-only front door.

### 6. Copy button swaps to checkmark on click (feedback cue)

```js
copyBtn.addEventListener('click', () => {
    linkOut.select();
    navigator.clipboard.writeText(linkOut.value);
    const orig = copyBtn.innerHTML;
    copyBtn.innerHTML = '<svg ...checkmark path.../>';
    setTimeout(() => { copyBtn.innerHTML = orig; }, 2000);
});
```

Without visible feedback, users tap Copy repeatedly and wonder if it
worked. The SVG-based icon swap preserves the icon button aesthetic while
giving confirmation.

### 7. Web Share API is progressively enhanced

```js
if (navigator.share) {
    shareBtn.hidden = false;
    shareBtn.addEventListener('click', () => {
        navigator.share({
            title: 'confession',   // or whatever your layer is called
            text: 'a confession for you.',
            url: linkOut.value,
        });
    });
}
```

Default hidden. Only revealed if the browser supports native share
sheets. Mobile users get the full share sheet; desktop users just see the
Copy button.

### 8. iOS audio playback unlock (if your layer plays audio at all)

Most layers don't play audio — the playback happens on ephemeral.website.
But if you ever want to preview a recording before sending, you must
prime the `<audio>` element inside a user gesture:

```js
playBtn.addEventListener('click', () => {
    const audio = new Audio(blobUrl);
    audio.volume = 0;
    audio.play().then(() => {
        audio.pause();
        audio.currentTime = 0;
        audio.volume = 1;
    });
    // ... later, audio.play() works without a fresh user gesture
});
```

Without this, iOS silently refuses to play the audio later.

### 9. AudioContext must be resumed on iOS

```js
const audioCtx = new AudioContext();
if (audioCtx.state === 'suspended') await audioCtx.resume();
```

iOS creates AudioContexts in the suspended state. Required for the
recording volume analyser and for any waveform computation you want to
do. Must be called inside the user gesture that created the context.

### 10. File input accept must be broadened

If your layer accepts file uploads instead of only recording:

```html
<input type="file" accept="audio/*,.mp3,.m4a,.wav,.aac,.ogg,.flac,.opus,.webm,.mp4">
```

iOS Files app doesn't reliably tag downloaded audio files with `audio/*`
MIME types. Explicit extensions catch the rest. Also validate in JS by
both MIME type AND filename extension:

```js
const audioExt = /\.(mp3|m4a|wav|aac|ogg|flac|opus|webm|mp4|aiff?)$/i;
const isAudio = file.type.startsWith('audio/') || audioExt.test(file.name);
```

### 11. Inputs must be 16px on mobile

```css
@media (max-width: 480px) {
    input, textarea {
        font-size: 16px;
    }
}
```

If an input has computed `font-size < 16px`, iOS zooms in on focus and
does NOT zoom back out. This override needs specificity high enough to
beat any class-level rules. Every input must be at least 16px on mobile.

### 12. Use 100dvh and safe-area insets

```css
body {
    min-height: 100vh;
    min-height: 100dvh;
    padding: env(safe-area-inset-top) env(safe-area-inset-right)
             env(safe-area-inset-bottom) env(safe-area-inset-left);
}
```

`100vh` is broken on iOS Safari — the viewport changes as the toolbar
shows and hides. `100dvh` is the dynamic viewport and does the right
thing. Safe-area insets keep content away from the notch and home
indicator. Also add `viewport-fit=cover` to your viewport meta tag.

---

## What you should not build

These are things the core client does that a layer does not need. Copying
them adds code without adding value.

### Playback-side protocols

Heartbeats, sessions, completion, pause countdowns, forward-only players,
the 3-second countdown before playback, the 1-second burn delay — all of
this is listener-side and happens on ephemeral.website when the recipient
clicks the shared link. Your layer never runs any of it.

### The 2-phase reservation flow

The scaffolds use a simpler single-phase upload: POST /upload, PUT to S3,
show the link. That's enough for a layer because the sender isn't typing
a slug and a note separately on the layer's page. If you want to add a
slug field to your layer, you can copy the 2-phase reservation pattern
from ephemeral.website — see the reference section below — but it's
optional. Most layers don't need it.

### Waveform computation

Waveforms only render on ephemeral.website's playback page. If you
compute them on the layer and include them in the upload, they'll show up
correctly when the recipient listens. But you can also just omit the
`waveform` field on upload and the player will show an empty fill bar.
Nothing else breaks.

### Account systems, handles, inboxes

No layer needs any of these. The whole premise is that the primitive is
stateless per-whisper. If a layer wants to add pseudonymous handles for
routing, that's a whole separate design problem — see the discussion in
the ephemeral.website conversation history. But for a v1 layer, you don't
need it.

---

## Minimum viable layer

Three files. About 300 lines total.

**`frontend/index.html`**

A record button, a send button, a result box with the share URL. Optional:
a title, subtitle, footer. See the confession.website scaffold for an
exact template — it is the minimum viable layer shape.

**`frontend/style.css`**

Themed colors, the chromatic aberration on the wordmark if you want it,
the 2-minute recording urgency timer animation, mobile safe areas,
16px inputs. See either scaffold.

**`frontend/script.js`**

```js
const API = 'https://ephemeral.website/api';

// 1. Record audio (with iOS MIME detection)
// 2. Validate size (<5MB)
// 3. POST /api/upload with { content_type }
// 4. PUT audio bytes to data.upload_url
// 5. Display `https://ephemeral.website/${data.token}` in the result box
// 6. Copy + Share buttons
```

That's the entire layer. No backend, no database, no deploy infrastructure
more complex than "host three static files somewhere."

For hosting: S3 + CloudFront, Cloudflare Pages, Netlify, or the same Go
Lambda + API Gateway pattern as ephemeral.website. Namecheap DNS with a
CNAME to whichever host you pick.

### The CORS allowlist

Before your layer can actually call the API from the browser, add your
origin to `infra/main.go` in the ephemeral.website repo:

```go
CorsConfiguration: &apigatewayv2.ApiCorsConfigurationArgs{
    AllowOrigins: pulumi.StringArray{
        pulumi.String("https://ephemeral.website"),
        pulumi.String("https://appreciation.website"),
        pulumi.String("https://confession.website"),
        pulumi.String("https://yourlayer.website"),  // add here
    },
    // ...
},
```

Then `cd infra && pulumi up`. Takes about 20 seconds.

---

## Reference: the core client's full protocol

This section documents what ephemeral.website's own frontend does. You do
not need to replicate it in a layer — it runs on ephemeral.website when
the recipient clicks the shared link. But if you are building an
alternative player (or just want to understand the design), here it is.

### Two-phase link creation

The sender-side flow on ephemeral.website is split into three phases to
hide all the network latency behind the user's typing:

**Phase 0 — Kickoff** (file select / recording complete):

```
POST /api/upload { content_type }
  → returns token, upload_url, s3_key

PUT  upload_url  (audio bytes)
  → S3 ingests the audio while the user is typing
```

The initial token is thrown away. Only the `s3_key` and the in-flight S3
PUT matter. By the time the user finishes typing the slug, the audio is
already in S3.

**Phase 1 — Reserve** (on slug blur / note focus):

```
POST /api/upload { slug, content_type, s3_key }
  → creates the real token with the user's slug
  → OR 409 if the slug is taken
```

If 409, the inline error shows on the note form. The user's audio is not
lost — they pick a different slug and phase 1 retries.

**Phase 2 — Commit** (on Send):

```
POST /api/upload { slug, note, waveform, content_type, s3_key }
  → server matches s3_key to the existing row, upserts note + waveform
  → same token, no collision
```

The share page is revealed synchronously before this POST returns.
Because phase 1 already created the token, the client knows the final URL
at Send time without waiting. The phase-2 POST is fire-and-forget.

The `s3_key` is the ownership primitive. Only the original uploader has
it (returned once from phase 0). Server-side upsert logic: same slug +
same s3_key = update in place; same slug + different s3_key = 409.

Reference: `frontend/ephemeral.js` → `kickoffOptimistic()`,
`reserveSlug()`, `upload()`.

### Two-phase consumption

The listener-side flow is also three phases, designed to prevent bots
and accidents from consuming a whisper:

**Phase 0 — Pre-check** (on page load):

```
GET /api/check/{token}
  → { exists, note, waveform, stream_url }
  → OR { exists: "false" }
```

Verifies the token exists, returns a preloadable S3 stream URL. If the
token is gone, show the gone state immediately without making the user
tap anything. `/check` does NOT burn the token; it's safe to call
repeatedly.

**Phase 1 — Prime** (on play button tap):

Start a 3-second countdown animation on the play pill (cancelable by
tapping again). In the same tap handler, create the `<audio>` element and
prime it at `volume = 0` for iOS unlock:

```js
audio = new Audio(stream_url);
audio.volume = 0;
audio.play().then(() => {
    audio.pause();
    audio.currentTime = 0;
    audio.volume = 1;
});
```

The token is NOT yet burned. If the listener cancels within 3 seconds, no
state has changed.

**Phase 2 — Commit** (after real playback begins):

When the countdown completes, call `audio.play()` for real. Monitor
`audio.currentTime` in a `requestAnimationFrame` loop. When
`currentTime >= 1`:

```
POST /api/session { token }
  → returns session_id
```

Then heartbeat every 5 seconds:

```
POST /api/heartbeat/{session_id}
```

On `<audio>`'s `ended` event:

```
POST /api/complete/{session_id}
  → server deletes the S3 object
```

Burning at 1 second of confirmed playback (rather than at tap) means bot
visits, preview checks, and accidental taps no longer consume whispers.

Reference: `frontend/ephemeral.js` → `initPlayer()`, `startPlaying()`,
`tickPlayback()`, `sendHeartbeat()`, `sendComplete()`.

### Pause semantics

When playback is paused, show a visible 15-second countdown ring
(matching the server's `pause_timeout`). Resume cancels it; expiry calls
complete and navigates to gone.

### Heartbeat failure tolerance

A single failed heartbeat (network blip, backgrounded tab) does NOT kill
the session. Only an explicit HTTP error response triggers `gone()`. This
makes playback resilient without compromising the one-listen guarantee.

### Session cleanup on unmount

Clear all timers (`heartbeatTimer`, `countdownTimer`, `playRAF`), pause
and release the `<audio>` element, remove animation classes. See
`frontend/ephemeral.js` → the `gone()` function for the full cleanup
order.

### UX polish the core adds that layers can skip

- Chromatic aberration on the wordmark (drifts cyan/magenta via
  animated text-shadow)
- Breathing background animation during playback (`@keyframes breathe`)
- Haptic vibrate on completion (`navigator.vibrate(200)`)
- Forward-only player (no scrub bar, read-only progress fill)
- "Record one back" primary CTA on completed playback (swap from
  outlined to filled with cyan/magenta glow)
- Note displayed during the 3-second countdown, then hidden during
  playback
- Slug live normalization on input (lowercase, spaces → hyphens, strip
  invalid, collapse runs)
- Required non-empty slug (mindfulness gate)
- Tab-content layout-shift lock (record/upload tabs same height)
- Golden-ratio upload box with two-line prompt

None of these are required for correctness. They are the pile of small
choices that make the core client feel like more than a working
prototype. Layers can adopt any or none of them; the scaffolds pick a few
per layer to give each a distinctive texture without requiring the full
set.

---

## One last thing

If you are copying a scaffold to make a new layer, the footer line
`so i made this website` is the intended tone for the whole shelf. Keep
it. It is more effective branding than any marketing copy could be. It
tells users exactly what this is: one person's handmade website, not a
product, not a platform, not a startup. That's the ethos that makes the
shelf cohere.
