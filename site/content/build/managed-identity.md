---
title: Remember returning participants
description: Restore a managed site's existing identity before displaying a person's contributions.
---

# Remember returning participants

This tutorial is for `pagelike host` embedded in an approved parent application.
Standalone `pagelike serve` uses its own [identity flow](/identity/).

Load the managed SDK and ask who is returning when the page opens:

```html
<p id="mine" role="status"></p>
<script src="/-/client.js"></script>
<script type="module">
  const person = await pagelike.identity();
  if (person) {
    const response = await fetch('/-/contributions', { cache: 'no-store' });
    if (response.ok) {
      const contributions = await response.json();
      document.querySelector('#mine').textContent =
        `You have ${contributions.length} contributions here.`;
    }
  }
</script>
```

The parent checks whether its signed-in person already joined this site. If so,
it provides a one-use ticket and the runtime creates this browser's private
session. The resulting opaque identity stays the same across devices. New and
signed-out visitors receive `null`, without a sign-in prompt. An unavailable
identity service also yields `null`; public reads and local play can continue.

Call `await pagelike.participate()` in the handler for an intentional save,
vote or upload. Then make the write. Restoration itself saves no contribution.
Use the returned identity to match existing entries in your data, or use the
runtime's own contribution listing as above. Do not trust localStorage as an
authorization check or assume receiving an identity means someone contributed.

Hosts implement the exact [parent message protocol](/hosting/). Keep that
restoration separate from prompts, and do not cancel it on the iframe's first
`load` event. The [native contract](/spec/hosting/) and browser tests cover
separate browser contexts, real partitioned cookies, forged replies, logout,
concurrent calls and missing parent responses.
