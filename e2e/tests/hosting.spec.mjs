import { test, expect } from '@playwright/test';
import { spawn, execFileSync } from 'node:child_process';
import { mkdtemp, rm, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import http from 'node:http';
import https from 'node:https';
import net from 'node:net';
import { once } from 'node:events';
import { state } from '../lib/pagelike.mjs';

test('managed iframe joins through its parent and keeps a partitioned private session', async ({ browser }) => {
  const data = await mkdtemp(join(tmpdir(), 'pagelike-host-browser-'));
  let used = false, revoked = false, proc;
  const issuer = http.createServer(async (req, res) => {
    const chunks=[]; for await (const c of req) chunks.push(c);
    const input=JSON.parse(Buffer.concat(chunks));
    if (req.headers.authorization !== 'Bearer identity' || input.site !== 'example') { res.writeHead(401).end(); return; }
    if (req.url === '/redeem') {
      if (used || input.ticket !== 'once-only') {res.writeHead(401).end(); return;}
      used=true; res.setHeader('Content-Type','application/json');
      res.end(JSON.stringify({subject:'opaque-person',session:'server-only-session',expiresAt:Date.now()+3_600_000}));return;
    }
    res.writeHead(!revoked && input.session==='server-only-session' ? 204 : 401).end();
  });
  issuer.listen(0,'127.0.0.1'); await once(issuer,'listening');
  const reserve=net.createServer();reserve.listen(0,'127.0.0.1');await once(reserve,'listening');
  const port=reserve.address().port;await new Promise(resolve=>reserve.close(resolve));
  const upstream=`http://127.0.0.1:${port}`;
  // A real TLS proxy keeps browser cookie handling intact. Intercepted browser
  // responses can turn Set-Cookie into automation-injected, unpartitioned cookies.
  execFileSync('openssl',['req','-x509','-newkey','rsa:2048','-nodes','-keyout',join(data,'key.pem'),'-out',join(data,'cert.pem'),'-subj','/CN=content.example','-days','1'],{stdio:'ignore'});
  const parent=`<iframe src="https://example.content.example/v/one/index.html"></iframe><button id="consent" hidden>Join this post</button><script>let pending;onmessage=e=>{if(e.source===document.querySelector('iframe').contentWindow&&e.origin==='https://example.content.example'&&e.data.type==='pagelike:participate'){pending=e.data.id;document.querySelector('#consent').hidden=false;}};document.querySelector('#consent').onclick=()=>document.querySelector('iframe').contentWindow.postMessage({type:'pagelike:participation',id:pending,ticket:'once-only'},'https://example.content.example');</script>`;
  const tls=https.createServer({key:await readFile(join(data,'key.pem')),cert:await readFile(join(data,'cert.pem'))},async(req,res)=>{
    try {
      if(req.headers.host==='player.example'){res.writeHead(200,{'Content-Type':'text/html'}).end(parent);return;}
      if(req.headers.host!=='example.content.example'){res.writeHead(404).end();return;}
      const chunks=[];for await(const c of req)chunks.push(c);
      const headers={...req.headers,authorization:'Bearer edge','x-pagelike-host':'example.content.example'};delete headers.host;
      const response=await fetch(upstream+req.url,{method:req.method,headers,body:chunks.length?Buffer.concat(chunks):undefined,redirect:'manual'});
      res.writeHead(response.status,Object.fromEntries(response.headers));res.end(Buffer.from(await response.arrayBuffer()));
    }catch{res.writeHead(503).end();}
  });
  tls.listen(0,'127.0.0.1');await once(tls,'listening');
  const proxy=http.createServer();const tunnels=new Set();
  proxy.on('connect',(req,socket,head)=>{
    if(!['player.example:443','example.content.example:443'].includes(req.url)){socket.destroy();return;}
    const target=net.connect(tls.address().port,'127.0.0.1',()=>{socket.write('HTTP/1.1 200 Connection Established\r\n\r\n');if(head.length)target.write(head);socket.pipe(target).pipe(socket);});
    tunnels.add(socket);tunnels.add(target);socket.on('close',()=>{tunnels.delete(socket);target.destroy();});target.on('close',()=>tunnels.delete(target));target.on('error',()=>socket.destroy());socket.on('error',()=>target.destroy());
  });
  proxy.listen(0,'127.0.0.1');await once(proxy,'listening');
  const context=await browser.newContext({ignoreHTTPSErrors:true,proxy:{server:`http://127.0.0.1:${proxy.address().port}`}});
  try {
    proc=spawn(state().bin,['host','--data',data,'--listen',`127.0.0.1:${port}`],{stdio:'ignore',env:{PATH:process.env.PATH,PAGELIKE_DOMAIN:'content.example',PAGELIKE_ADMIN_TOKEN:'admin',PAGELIKE_EDGE_TOKEN:'edge',PAGELIKE_IDENTITY_TOKEN:'identity',PAGELIKE_IDENTITY_URL:`http://127.0.0.1:${issuer.address().port}`,PAGELIKE_FRAME_ORIGINS:'https://player.example'}});
    await expect.poll(async()=>{try{return (await fetch(upstream)).status;}catch{return 0;}}).toBe(401);
    const files={
      'index.html': `<script src="/-/client.js"></script><button id="add">Leave a light</button><p id="status">Just looking</p><script>document.querySelector('#add').onclick=async()=>{try{await pagelike.participate();let r=await fetch('/data/lights.html',{method:'POST',headers:{'Content-Type':'text/html',Range:'selector=#lights'},body:'<li>A light</li>'});document.querySelector('#status').textContent=r.ok?'Light saved':'Save failed '+r.status;}catch{document.querySelector('#status').textContent='Join failed';}}</script>`,
      'rules.html': '<div itemscope itemtype="https://pagelove.org/AuthorizationRule"><meta itemprop="actor" content="users"><meta itemprop="resource" content="/data/*"><meta itemprop="method" content="POST"><meta itemprop="action" content="Allow"></div>',
      'data/lights.html':'<ul id="lights"></ul>',
    };
    const install=await fetch(upstream+'/v1/sites/example',{method:'PUT',headers:{Authorization:'Bearer admin'},body:JSON.stringify({generation:1,version:'one',participation:true,expiresAt:0,deleted:false,temporary:false,files:Object.fromEntries(Object.entries(files).map(([p,b])=>[p,Buffer.from(b).toString('base64')]))})});
    expect(install.status).toBe(204);
    const page=await context.newPage();await page.goto('https://player.example/');
    const frame=page.frameLocator('iframe');
    await frame.getByRole('button',{name:'Leave a light'}).click();
    await expect(page.getByRole('button',{name:'Join this post'})).toBeVisible();
    expect(used).toBe(false);
    await page.getByRole('button',{name:'Join this post'}).click();
    await expect(frame.locator('#status')).toHaveText('Light saved');
    const content=page.frames().find(f=>f.url().startsWith('https://example.content.example/'));
    expect(await content.evaluate(()=>document.cookie)).toBe('');
    const me=await content.evaluate(()=>fetch('/-/me').then(r=>r.json()));
    expect(me.participant).toBe('opaque-person');
    expect(JSON.stringify(me)).not.toContain('server-only-session');
    const cookies=await context.cookies('https://example.content.example/');
    expect(cookies).toHaveLength(1);expect(cookies[0].name).toBe('__Host-session');
    expect(cookies[0].httpOnly&&cookies[0].secure).toBe(true);
    expect(cookies[0].partitionKey).toBe('https://player.example');
    await content.evaluate(()=>localStorage.setItem('local-play','works'));
    expect(await content.evaluate(()=>localStorage.getItem('local-play'))).toBe('works');
    revoked=true;
    expect(await content.evaluate(()=>fetch('/data/lights.html',{method:'POST',headers:{'Content-Type':'text/html',Range:'selector=#lights'},body:'<li>Late</li>'}).then(r=>r.status))).toBe(401);
    expect(await content.evaluate(()=>fetch('/data/lights.html').then(r=>r.text()))).toContain('A light');
  } finally {
    await context.close();for(const socket of tunnels)socket.destroy();
    await new Promise(resolve=>proxy.close(resolve));tls.closeAllConnections();await new Promise(resolve=>tls.close(resolve));
    if(proc){const exited=once(proc,'exit');proc.kill('SIGTERM');await exited;}
    await new Promise(resolve=>issuer.close(resolve));await rm(data,{recursive:true,force:true});
  }
});
