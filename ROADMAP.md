# Roadmap

## Cross-site code sharing with confession.website

**Decision (2026-04-09):** copy plumbing between repos, don't extract
a shared library yet.

### Context

Two sister sites share stack, AWS account, DNS provider, and
design discipline:

- **ephemeral.website** — one-shot outbound voice. Token + Session
  two-table pattern with heartbeat-driven pause detection.
- **confession.website** — turn-based rally with burn-on-listen and
  5–7 min reply windows. Single META item per slug, flipped in
  place.

Both run on Go Lambda + DynamoDB + S3 + CloudFront + API Gateway
v2. Both use URL-as-credential, content-free logging, edge rate
limiting, one Lambda per HTTP endpoint, and a local `internal/`
package for DDB/S3 helpers.

### What we're doing instead of extracting

Each site keeps its own `internal/` package. When confession.website
is built out, its plumbing helpers (Store struct, NewStore,
PresignUpload, PresignStream, response.JSON / response.Error, slug
canonicalization, base64 audio decode) are copy-pasted from
ephemeral's `backend/internal/` with a docstring at the top of each
file saying "pattern from ephemeral.website's store.go as of commit
<sha>". That gives us provenance for later diffing without any
runtime or dependency coupling.

### Why not extract now

**Rule of three.** We have two consumers. Abstractions extracted
from two similar things usually fit neither cleanly, because you
can't tell which similarities are coincidental vs structural until
a third case triangulates against them. Below N=3, copy-paste is
the honest choice. Two apps with ~150 lines of shared boilerplate
and one developer is not where abstraction pays off.

**Composition, not inheritance, at the code-sharing layer.** If we
ever do extract, the shared library should provide *building
blocks*, not *base types*. No `AudioBurnService` superclass that
both apps extend. Just `store.Store`, `response.JSON(code, body)`,
`slug.Canonicalize(s)`, `audio.DecodeAndValidate(b64, max, mimes)`.
The library should know nothing about tokens, sessions, meta items,
burn semantics, or any domain concept.

The reason: the genuine commonality between ephemeral and
confession is at the *plumbing* layer (AWS primitives, HTTP shapes,
slug grammar, base64 handling). The divergence is at the *semantic*
layer (state machines, burn semantics, temporal models). A library
that stays at the plumbing layer composes cleanly into either app.
A library that tries to abstract the semantic layer becomes a leaky
superset that serves neither app well.

This is composition over inheritance applied to cross-app code
reuse: share *what is actually shared* (plumbing), nothing more.

**Low coordination cost today.** One developer, two small apps,
narrow shared surface (~150–300 lines of boilerplate). Fixing a
bug in two places is trivially cheap at this scale. The
coordination cost of maintaining a third repo, versioning, and
publishing exceeds the benefit.

### Tripwires — when to reconsider

Reconsider extracting when **any** of the following becomes true:

1. **A third voice-burn site appears.** Three consumers make the
   abstraction benefit clear; two consumers make it speculative.
2. **The same bug gets fixed in both repos three times.** First
   two are copy-paste working as expected. Third is the signal
   that drift is now costing more than extraction would.
3. **Security coordination across both sites becomes necessary.**
   If a CVE in a crypto primitive or a CSRF mitigation needs to
   roll out to both sites simultaneously, a versioned library is
   much easier to coordinate than two copies.
4. **A common-layer refactor becomes impossible** because the two
   copies have drifted in subtly incompatible ways. That's the
   "extract now or never" moment.

### If we extract: the shape

**Repo layout:** add a sibling module at
`~/Documents/audio-media-common/`. Use `go.work` at the parent
level to wire the three modules during local dev.

```
~/Documents/
  audio-media.work            ← workspace file
  ephemeral.website/
  confession.website/
  audio-media-common/         ← new repo/module
    go.mod                    ← module github.com/kimjune01/audio-media-common
    store/                    ← DDB + S3 client setup, presign helpers
    response/                 ← API Gateway v2 response shapes
    slug/                     ← canonicalization + validation
    audio/                    ← base64 decode, size + MIME validation
    logging/                  ← content-free log wrapper
```

`audio-media.work`:

```
go 1.25

use (
    ./ephemeral.website
    ./confession.website
    ./audio-media-common
)
```

**What goes in the common module:** only plumbing. `store.Store`
holds AWS clients and env config. `response.JSON(code, body)`
returns an `events.APIGatewayV2HTTPResponse`. `slug.Canonicalize(s)`
returns a validated string or an error. `audio.DecodeAndValidate`
decodes base64 + checks size + checks MIME. Maybe ~150–300 lines
total.

**What stays in each app's `internal/`:** state types (`Token`,
`Session`, `Meta`, `ReplyCode`), business logic (`CreateToken`,
`BurnToken`, `PutMeta`, `MintReplyCode`), and anything that encodes
the app's state machine.

**Deploy:** each app's build step runs `go mod vendor` inside its
own directory, baking a pinned snapshot of the common module into
the Lambda zip. No runtime dependency on a published version; each
deploy is self-contained.

**No lock-in:** if the abstraction turns out wrong, the library
can be deleted and its files copied back into each app's
`internal/` in about five minutes. Worth re-checking at every
review: is the library still earning its keep, or has drift made
the extraction leaky?

### See also

- `confession.website/SPEC.md` §"Relationship to ephemeral.website"
  — describes the separate-runtime, shared-conventions stance at
  the deployment layer.
- `ephemeral.website/backend/internal/store.go` — the current
  source of truth for the plumbing patterns. Copy from here when
  starting a new sibling.
