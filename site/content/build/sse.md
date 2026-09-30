---
title: Live updates (SSE)
description: Subscribe to a document and stream its changes.
---

# Tutorial — Live updates (SSE)

Every acknowledged write pushes an event to subscribed clients. The
stream is server-sent events (SSE), gated by an opaque token so
the writer doesn't receive its own echo.

The full spec is at [`/spec/sse/`](/spec/sse/).

## Subscribe

```sh
curl -N -H 'Accept: text/event-stream' \
  http://demo.localhost:8787/index.html
```

The response carries `pagelove-connection` and `pagelove-event` types
plus a `Last-Event-ID` you can resume from:

```
id: 1
: connected

id: 2
event: pagelove-connection
data: {"token":"…","expires":"…"}

```

## Read at the moment of subscription

Add `Range: selector=#some-block` to subscribe to a single selector.
The first event you receive is a `pagelove-read` carrying the
current contents of that selector. Subsequent events are
`pagelove-mutation` items for the same selector's children.

```sh
curl -N -H 'Accept: text/event-stream' \
  -H 'Range: selector=#entries' \
  http://demo.localhost:8787/index.html
```

## Reconnect with replay

Use `Last-Event-ID: N` to resume after a connection drop. The
server replays events from after `N` until the cursor catches up,
then continues live. Events older than the retention window
(default 10 minutes) are pruned on reconnect; the client sees a
`pagelove-reset` event and re-fetches with `GET`.

## Reset by retention

The retention window is configurable per site. The CLI:
`pagelike events prune --site NAME --older-than 10m`. Clients
reconnecting from before the prune receive a reset.

## Slow-consumer protection

Each open stream holds a small bounded buffer. A client that can't
keep up is closed; the next reconnect gets a reset event.

## What to read next

- [`/spec/sse/`](/spec/sse/) — full SSE behavioural contract.
- [`/examples/poll/`](/examples/poll/) — a poll with live tally
  updates.
- [`/examples/sky/`](/examples/sky/) — a media board with per-post
  live views.
- [`/spec/reacting/`](/spec/reacting/) — for the case where the
  server is the consumer, not the producer.
