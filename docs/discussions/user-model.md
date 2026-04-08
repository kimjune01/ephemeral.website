# User model: none

    All I want to do
    is to show you
    I don't know who
    that which is my essence
    that is the transmission
    I am now sending to you

"I don't know who" is about **the platform** not knowing. Voice carries
identity. The listener knows who sent the whisper the instant they press
play, because the sender is someone they know well enough — in person or
by voice — to recognize. If they don't, the app is being misused, and the
correct response is to discard.

## The factoring

The platform does storage + burn. Delivery is off-platform. This is not a
limitation to fix, it's the correct factoring. Addressability, discovery,
and continuity all live at the delivery layer — iMessage, Signal, a note
on a napkin, a verbal "go to confession.website slash for-cute-barista."
The platform never enters the delivery business except to close a volley.

## The cafe (the ethical core)

I'm at a cafe. I chat up the barista. I like her and want to say something.
My options used to be:

- **Ask for her number.** Creepy. An unwanted rejection opportunity. I'd
  need her name. I'd carry the obligation of when and how to deliver a
  message; she'd carry the matching anticipation. Once I have her number,
  power is one-sided. Debt carried twice over.
- **Say nothing.** The usual outcome. The moment passes.

With ephemeral.website there's a third option. I record a whisper, mint
`confession.website/for-cute-barista`, and tell her the URL on the way
out. The slug is both address and private signal — guessable only in
context, meaningless to anyone else. Three outcomes:

- **She never goes.** Nothing happened. I can't even tell for sure.
- **She listens once and does nothing.** The URL 404s. If I check later,
  the 404 is my read receipt. No reply, no obligation, no further contact.
  I'm free.
- **She listens and replies.** Volley.

Every branch eliminates debt on both sides. No names, no numbers, no power
asymmetry, no anticipation, no obligation to respond, no harassment vector.
The message happens or it doesn't, and both of us keep our dignity in every
case. That is what the app is for.

## The volley: the single delivery carve-out

When the listener replies, the sender needs to know. A push subscription,
owned by the sender, bound to the paired reply token of this one whisper,
triggerable only by the one recipient of this one volley, expiring when
the volley completes or the TTL hits. The platform enters the delivery
business only to close a loop that started off-platform.

The subscription is not a recipient address. The sender cannot be targeted
by anyone they haven't already whispered to, and the reply channel dies
after one round. Strangers cannot initiate. Sarahah-style harassment is
structurally impossible because there is no addressable recipient, ever —
only addressable senders, and only for as long as their own whisper hasn't
been answered. One round, then terminate. Extended back-and-forth belongs
in whatever channel the relationship already lives in.

On iOS Safari, push requires Add-to-Home-Screen. That's fine: the A2HS
prompt is optional and contextual ("add to home screen to get notified
when someone replies"), not a prerequisite. If the sender declines or is
on a platform without push, the reply waits on the server until they
return. Graceful degradation, no blocking flow.

## What is rejected, and why

- **Handles chosen by recipients.** Any recipient-owned primitive is a
  stranger-targetable pointer. Sarahah's failure vector.
- **Discovery, search, directories.** The delivery channel already solves
  discovery. Platform discovery collapses "I don't know who."
- **Inboxes, archives, threads.** The whisper is the unit. It burns.
  Anything that outlives it is a social graph wearing a disguise.
- **Notifications for anything except replies to your own whispers.**
  Any broader notification surface re-opens the delivery layer.
- **Pseudonymous continuity as a server primitive.** Continuity is carried
  by the voice. If the server helped, the server would have to know — the
  exact commitment it refuses.

## Open implementation question

**Volley TTL.** Leaning 24 hours: short enough that the reply lands while
the listen is still emotionally live, long enough that the recipient can
sleep on it. Longer turns the volley into ambient communication, which is
someone else's job. Belongs in the volley primitive spec, not here.
