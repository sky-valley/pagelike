// The trusted parent introduces a one-use pass. Authored code receives only a
// post-local identity. The runtime replaces __FRAME_ORIGINS__ when serving this.
(() => {
  const parents = __FRAME_ORIGINS__;
  for (const origin of parents) window.parent.postMessage({ type: 'pagelike:ready' }, origin);
  let pending;
  async function participate() {
    const me = await fetch('/-/me', { credentials: 'same-origin', cache: 'no-store' });
    if (me.ok) { const value = await me.json(); if (value.authenticated) return value.participant; }
    if (window.parent === window) throw Error('Open this post in its player to participate.');
    if (pending) return pending;
    pending = new Promise((resolve, reject) => {
      const id = crypto.randomUUID();
      const finish = (error, value) => { clearTimeout(timer); window.removeEventListener('message', receive); pending = undefined; error ? reject(error) : resolve(value); };
      const receive = async event => {
        if (event.source !== window.parent || !parents.includes(event.origin) || event.data?.type !== 'pagelike:participation' || event.data?.id !== id) return;
        window.removeEventListener('message', receive);
        if (typeof event.data.ticket !== 'string') { finish(Error(event.data.error === 'signin' ? 'Sign in through the player to participate.' : 'Participation is unavailable.')); return; }
        try {
          const response = await fetch('/-/session', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ticket: event.data.ticket }) });
          if (!response.ok) throw Error('Participation is unavailable. Try again.');
          finish(null, (await response.json()).participant);
        } catch (error) { finish(error); }
      };
      const timer = setTimeout(() => finish(Error('Participation request expired. Try again.')), 60000);
      window.addEventListener('message', receive);
      for (const origin of parents) window.parent.postMessage({ type: 'pagelike:participate', id }, origin);
    });
    return pending;
  }
  window.pagelike = Object.freeze({ participate });
})();
