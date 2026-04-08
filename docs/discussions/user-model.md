# Bootstrap: user model for the layers

Paste the block below into a new conversation to start a discussion about
what (if any) user primitive the layers should have. This is a conversation
starter, not a decision document.

---

I'm building a small constellation of voice websites centered on one
primitive: record audio, get a one-time link, the recipient plays it once,
then the audio is deleted and the URL 404s. 404 is indistinguishable from
"never existed."

The core is ephemeral.website — live, deployed, Go Lambda + DynamoDB + S3 +
Pulumi, public v1 API at /api. Two initial layers are scaffolded:
confession.website and appreciation.website. Each layer is a themed front
door that calls the core's public API to create whispers. Layers are
send-only; playback always happens on ephemeral.website.

I want to discuss the user model for the layers.

Currently the answer is "there is no user model." A user shows up, records,
gets a link, shares it out-of-band (iMessage, Signal, whatever), leaves. No
sign-in, no return visit, no database row representing them. This honors
the philosophical commitment of the whole project, which comes from a poem
I wrote in 2016:

    All I want to do
    is to show you
    I don't know who
    that which is my essence
    that is the transmission
    I am now sending to you

The load-bearing line is "I don't know who." Neither the sender nor the
recipient is a "who" during the transmission. Profile-less is an
ontological commitment, not a privacy feature. There is no identity to
defer or conceal, because identity was never the point — only the act of
transmission is.

But I keep coming back to the question of whether there's a *lightweight*
user primitive that still honors this, and unlocks things the current
stateless design can't do. Specifically:

1. Addressability. Right now a sender has to share the one-time link via
   some other app (iMessage, Signal, DM). If the recipient had any kind of
   stable pointer — a handle, a mailbox, something — the sender could
   deliver the whisper directly from the layer without leaving it. But any
   such pointer is a "who," which the poem refuses.

2. Volley. A completed listen shows a "record one back" CTA. It currently
   just opens the layer's own record UI with no idea who to send it to.
   A lightweight primitive could let the reply route back to the original
   sender without requiring them to share a new outbound link.

3. Pseudonymous continuity. A creator who wants to make multiple whispers
   under a stable voice — not a profile or a follower base, just a
   consistent "from" — has no way to do this currently. Listeners hear
   anonymous whispers every time even if they're from the same person.

4. Decoupling delivery from out-of-band channels. Email does this. A
   stranger can find you by name because email is addressable. But email
   brings deliverability, spam, real identity, and accounts.

Shapes I've already considered and am uncertain about:

- **Addressable handles** (ephemeral.website/@june with cookie-based auth,
  copy-pasteable master key for cross-device). Felt like smuggling in
  accounts even though the server-side state is just handle → hash.

- **Inbox-less routing**: each handle holds at most one pending whisper.
  New whisper overwrites the old one. Preserves ephemerality at the inbox
  level but still requires persistent state and still creates a targetable
  recipient, which was the Sarahah/Lipsi/NGL failure vector.

- **Paired tokens**: sender creates two linked tokens, keeps one privately,
  shares the other. Reply uses the kept token. Fully stateless on the
  identity side, addressable only for the duration of the volley. I like
  this one but it only solves the immediate-reply case, not the general
  addressability question.

- **Pure stateless**: same as current. Honest. Cheap. Honors "I don't know
  who" completely. Forces the delivery channel out-of-band forever, which
  might be correct.

Non-negotiables I've committed to:

- No email addresses as user IDs (pulls in deliverability, spam, real
  identity, regulatory surface)
- No phone numbers, no OAuth, no Google sign-in
- No persistent user-owned content — any inbox must be single-whisper or
  ephemeral, never an archive
- No discovery, search, suggestions, directory, or "people you may know"
- No analytics on individuals
- No feeds, follows, karma, likes, or reactions
- No social graph stored on the platform
- Layers may add thin state (e.g. a handles table) but the core cannot
- I used to work at Lipsi (a 2018-era anonymous messaging app). I have
  seen every harassment vector that anonymous-to-known messaging creates
  and I am not willing to rebuild them.

Questions I want to explore:

1. What is the cheapest, most commitment-free user primitive that lets a
   whisper find its way to a specific person who isn't visible to a
   stranger — without introducing a social graph or an inbox?

2. Is there a version of "pseudonymous continuity" that lets a creator
   have a stable voice across multiple whispers without becoming a
   profile? What does that primitive actually look like in the UI?

3. Where is the slippery slope? Handles feel fine in isolation. But the
   moment I add "find whisperghost's latest whisper" I've built a feed.
   The moment I add "see who your friends follow" I've built Twitter.
   Which specific primitives are load-bearing for the good thing and
   which ones are the first steps toward the bad thing?

4. Is the right answer actually "nothing — share the link via whatever
   channel you already use"? If so, I want to understand *why* clearly
   enough to stop returning to this question every week.

5. Does the two-layer architecture (ephemeral = core, appreciation/
   confession = layers) give us a way to experiment with a user model on
   one layer without touching the core? What's the cleanest boundary that
   lets the core stay pure while layers try different things?

Please start by pushing back on my assumptions. I am close enough to this
that I probably have blind spots about which of my non-negotiables are
actually load-bearing and which are just aesthetic preferences I've
convinced myself are structural.
