/**
 * ACC-UP-1 (docs/spec/apps.md §8.2): beta-js's own SSE test set-up
 * (test/helpers/dom.mjs: jsdom + FakeEventSource + a fresh pagelove/sse.mjs),
 * fed with the events of a REAL pagelike stream instead of hand-written
 * payloads. Writes are made through pagelike by another client; the DOM
 * outcome in the jsdom page is asserted.
 *
 * This file is pagelike's adapter, copied next to beta-js's tests in a
 * scratch working copy and run with the repository's own test command
 * (`node --import ./test/helpers/register.mjs --test`). The environment
 * names the document: PAGELIKE_DOC (absolute URL).
 */
import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import { setupSseClient } from './helpers/dom.mjs';

const DOC = process.env.PAGELIKE_DOC;
const INITIAL = '<ul id="list"><li id="a">A</li><li id="b">B</li><li id="c">C</li></ul>';
const streams = [];
after(() => streams.forEach((s) => s.abort()));

/** Open a real stream to pagelike and forward every event to `source`. */
async function pipe(source) {
    const ctl = new AbortController();
    streams.push(ctl);
    const res = await fetch(DOC, { headers: { Accept: 'text/event-stream' }, signal: ctl.signal });
    assert.equal(res.status, 200);
    const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
    let buf = '';
    let connected;
    const ready = new Promise((r) => { connected = r; });
    (async () => {
        try {
            for (;;) {
                const { value, done } = await reader.read();
                if (done) return;
                buf += value;
                let i;
                while ((i = buf.indexOf('\n\n')) >= 0) {
                    const block = buf.slice(0, i);
                    buf = buf.slice(i + 2);
                    let type = 'message';
                    const data = [];
                    for (const line of block.split('\n')) {
                        if (line.startsWith('event:')) type = line.slice(6).trim();
                        else if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, ''));
                    }
                    if (type === 'pagelove-connection') connected();
                    if (data.length) source.emit(type, data.join('\n'));
                }
            }
        } catch { /* aborted */ }
    })();
    await ready;
}

async function write(method, headers, body) {
    const res = await fetch(DOC, { method, headers, body });
    assert.ok(res.ok, `${method} ${JSON.stringify(headers)} -> ${res.status}`);
}

async function until(pred, what) {
    const end = Date.now() + 3000;
    while (!pred()) {
        if (Date.now() > end) assert.fail(`timed out waiting for ${what}`);
        await new Promise((r) => setTimeout(r, 20));
    }
}

const ids = (document) => [...document.querySelectorAll('#list > li')].map((li) => li.id + ':' + li.textContent);

test('server-produced POST, PUT, DELETE and MOVE events drive the SSE client', async () => {
    await write('PUT', { 'Content-Type': 'text/html', Range: 'selector=#list' }, INITIAL);
    const { document, source } = await setupSseClient(INITIAL);
    await pipe(source);

    await write('POST', { 'Content-Type': 'text/html', Range: 'selector=#list' }, '<li id="d">D</li>');
    await until(() => document.getElementById('d'), 'POST');
    assert.deepEqual(ids(document), ['a:A', 'b:B', 'c:C', 'd:D']);

    await write('PUT', { 'Content-Type': 'text/html', Range: 'selector=#b' }, '<li id="b">B2</li>');
    await until(() => document.getElementById('b')?.textContent === 'B2', 'PUT');

    await write('DELETE', { Range: 'selector=#a' });
    await until(() => !document.getElementById('a'), 'DELETE');

    await write('MOVE', { Range: 'selector=#d', Destination: new URL(DOC).pathname, 'Destination-Range': 'selector=#b; placement=before' });
    await until(() => ids(document)[0] === 'd:D', 'MOVE');
    assert.deepEqual(ids(document), ['d:D', 'b:B2', 'c:C']);

    // The jsdom DOM now equals the stored element.
    const stored = await (await fetch(DOC, { headers: { Range: 'selector=#list' } })).text();
    const normalise = (s) => s.replace(/\s+/g, '');
    assert.equal(normalise(document.getElementById('list').outerHTML), normalise(stored));
});
