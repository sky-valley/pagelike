import { test, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';

// Exercise the actual SDK with native cross-origin windows. Only HTTP authority
// responses are fixtures; hosting.spec.mjs covers real cookies and persistence.
test('identity binds the parent and request, coalesces calls, and fails quietly', async ({ page }) => {
  const client = (await readFile(new URL('../../internal/hosting/client.js', import.meta.url), 'utf8'))
    .replaceAll('__FRAME_ORIGINS__', JSON.stringify(['https://parent.example']));
  let authenticated = false, unavailable = false;
  const tickets = [];
  await page.route('https://**/*', async route => {
    const url = new URL(route.request().url());
    if (url.hostname === 'parent.example') return route.fulfill({ contentType: 'text/html', body: url.pathname === '/peer' ? '<p>Sibling</p>' : `<iframe src="https://site.example/"></iframe><iframe src="https://parent.example/peer"></iframe><iframe src="https://evil.example/"></iframe><script>window.requests=[];onmessage=e=>{if(e.source===frames[0]&&e.origin==='https://site.example'&&e.data.type!=='pagelike:ready')requests.push(e.data)};</script>` });
    if (url.hostname === 'evil.example') return route.fulfill({ contentType: 'text/html', body: '<p>Untrusted</p>' });
    if (url.pathname === '/-/client.js') return route.fulfill({ contentType: 'text/javascript', body: client });
    if (url.pathname === '/-/me') return route.fulfill({ status: unavailable ? 503 : 200, json: { authenticated, participant: authenticated ? 'existing-person' : '' } });
    if (url.pathname === '/-/session') {
      const { ticket } = route.request().postDataJSON(); tickets.push(ticket);
      authenticated = ticket === 'valid-pass';
      return route.fulfill({ status: authenticated ? 200 : 401, json: { participant: 'existing-person' } });
    }
    return route.fulfill({ contentType: 'text/html', body: '<script src="/-/client.js"></script>' });
  });
  await page.clock.install();
  await page.goto('https://parent.example/');
  const child = page.frames().find(frame => frame.url() === 'https://site.example/');
  await child.evaluate(() => { window.result = Promise.all([pagelike.identity(), pagelike.identity(), pagelike.participate()]); });
  await expect.poll(() => page.evaluate(() => requests.length)).toBe(1);
  const request = await page.evaluate(() => requests[0]);
  expect(request.type).toBe('pagelike:identity');
  // Same parent origin with the wrong source, and a completely wrong origin.
  for (const url of ['https://parent.example/peer', 'https://evil.example/']) {
    await page.frames().find(frame => frame.url() === url).evaluate(({ id }) => {
      parent.frames[0].postMessage({ type: 'pagelike:participation', id, ticket: 'forged' }, 'https://site.example');
    }, request);
  }
  await page.evaluate(({ id }) => {
    frames[0].postMessage({ type: 'pagelike:participation', id: 'wrong-id', ticket: 'forged' }, 'https://site.example');
    frames[0].postMessage({ type: 'pagelike:participation', id, ticket: 'valid-pass' }, 'https://site.example');
  }, request);
  expect(await child.evaluate(() => window.result)).toEqual(['existing-person', 'existing-person', 'existing-person']);
  expect(tickets).toEqual(['valid-pass']);
  expect(await child.evaluate(() => pagelike.identity())).toBe('existing-person');
  expect(await page.evaluate(() => requests.length)).toBe(1);
  unavailable = true;
  expect(await child.evaluate(() => pagelike.identity())).toBeNull();
  unavailable = false; authenticated = false;
  await child.evaluate(() => { window.result = pagelike.identity(); });
  await expect.poll(() => page.evaluate(() => requests.length)).toBe(2);
  await page.clock.runFor(5100);
  expect(await child.evaluate(() => window.result)).toBeNull();
  expect(tickets).toEqual(['valid-pass']);
});
