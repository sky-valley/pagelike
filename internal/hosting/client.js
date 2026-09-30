// The trusted parent introduces a one-use pass. Authored code receives only a
// post-local identity. The runtime replaces __FRAME_ORIGINS__ when serving this.
(() => {
  const parents = __FRAME_ORIGINS__;
  for (const origin of parents) window.parent.postMessage({ type: 'pagelike:ready' }, origin);
  let restoring, joining;
  async function currentIdentity() {
    const me = await fetch('/-/me', { credentials: 'same-origin', cache: 'no-store', signal: AbortSignal.timeout(5000) });
    if (!me.ok) throw Error('Identity is unavailable.');
    const value = await me.json();
    return value.authenticated && typeof value.participant === 'string' && value.participant ? value.participant : null;
  }
  async function request(type, timeout) {
    if (window.parent === window) throw Error('Open this post in its player to participate.');
    return new Promise((resolve, reject) => {
      const id = crypto.randomUUID();
      const controller = new AbortController();
      let finished = false;
      const finish = (error, value) => {
        if (finished) return;
        finished = true; clearTimeout(timer); controller.abort(); window.removeEventListener('message', receive);
        error ? reject(error) : resolve(value);
      };
      const receive = async event => {
        if (event.source !== window.parent || !parents.includes(event.origin) || event.data?.type !== 'pagelike:participation' || event.data?.id !== id) return;
        window.removeEventListener('message', receive);
        if (typeof event.data.ticket !== 'string') { finish(Error(event.data.error === 'signin' ? 'Sign in through the player to participate.' : 'Participation is unavailable.')); return; }
        try {
          const response = await fetch('/-/session', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ticket: event.data.ticket }), signal: controller.signal });
          if (!response.ok) throw Error('Participation is unavailable. Try again.');
          const value = await response.json();
          if (typeof value.participant !== 'string' || !value.participant) throw Error('Identity is unavailable.');
          finish(null, value.participant);
        } catch (error) { finish(error); }
      };
      const timer = setTimeout(() => finish(Error('Participation request expired. Try again.')), timeout);
      window.addEventListener('message', receive);
      for (const origin of parents) window.parent.postMessage({ type, id }, origin);
    });
  }
  // R-HOST-1: restoration never prompts or enrolls a first-time visitor. The
  // parent authority decides whether a previous identity exists for this site.
  function identity() {
    if (joining) return joining.catch(() => null);
    if (!restoring) restoring = (async () => {
      try { return await currentIdentity() || await request('pagelike:identity', 5000); }
      catch { return null; }
    })().finally(() => { restoring = undefined; });
    return restoring;
  }
  function participate() {
    if (!joining) joining = (async () => {
      const me = restoring ? await restoring : await currentIdentity();
      return me || await request('pagelike:participate', 60000);
    })().finally(() => { joining = undefined; });
    return joining;
  }
  window.pagelike = Object.freeze({ identity, participate });
})();
