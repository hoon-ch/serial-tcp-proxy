import { createServer, createConnection } from 'node:net';
import { spawn } from 'node:child_process';
import assert from 'node:assert/strict';

const args = process.argv.slice(2);
if (args.length !== 2 || args[0] !== '--bin') {
  console.error('Usage: bun contract.mjs --bin <path-to-server-binary>');
  process.exit(2);
}
const binary = args[1];
const credentials = { username: 'contract-user', password: 'contract-secret' };
const basic = 'Basic ' + Buffer.from(`${credentials.username}:${credentials.password}`).toString('base64');
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
const timeout = (promise, ms = 4000) => Promise.race([promise, delay(ms).then(() => { throw new Error(`timeout after ${ms}ms`); })]);
let passed = 0, failed = 0;
async function check(name, fn) {
  try { await fn(); passed++; console.log(`PASS ${name}`); }
  catch (err) { failed++; console.log(`FAIL ${name}: ${String(err.message).replace(/\s+/g, ' ')}`); }
}
function shape(obj, spec) {
  assert.ok(obj && typeof obj === 'object' && !Array.isArray(obj));
  assert.deepEqual(Object.keys(obj).sort(), Object.keys(spec).sort());
  for (const [key, type] of Object.entries(spec)) assert.equal(typeof obj[key], type, key);
}
const statusShape = { upstream_state: 'string', upstream_addr: 'string', listen_addr: 'string', connected_clients: 'number', max_clients: 'number', start_time: 'string' };
function status(data) { shape(data, statusShape); assert.equal(data.upstream_state, 'Connected'); }
async function listen(server) { await new Promise((resolve, reject) => server.once('error', reject).listen(0, '127.0.0.1', resolve)); return server.address().port; }
async function freePort() { const server = createServer(); const port = await listen(server); await new Promise(resolve => server.close(resolve)); return port; }
function socketQueue(socket) {
  let bytes = Buffer.alloc(0), ended = false;
  socket.on('data', data => { bytes = Buffer.concat([bytes, data]); });
  socket.on('error', () => { ended = true; });
  socket.on('close', () => { ended = true; });
  return {
    socket,
    async take(expected) {
      const target = Buffer.from(expected);
      await timeout((async () => { while (bytes.length < target.length && !ended) await delay(10); })());
      assert.deepEqual(bytes.subarray(0, target.length), target);
      bytes = bytes.subarray(target.length);
    },
    async quiet() { await delay(200); assert.equal(bytes.length, 0, 'unexpected forwarded bytes'); },
    async closed() { await timeout((async () => { while (!ended) await delay(10); })()); },
  };
}
async function waitFor(fn) { return timeout((async () => { for (;;) { const value = await fn(); if (value) return value; await delay(30); } })(), 6000); }
function wsMessages(ws) {
  const messages = [];
  ws.addEventListener('message', e => messages.push(JSON.parse(e.data)));
  return async predicate => waitFor(() => { const i = messages.findIndex(predicate); return i < 0 ? null : messages.splice(i, 1)[0]; });
}
async function openWebSocket(url, headers) {
  const ws = new WebSocket(url, { headers });
  const next = wsMessages(ws);
  await timeout(new Promise((resolve, reject) => { ws.addEventListener('open', resolve, { once: true }); ws.addEventListener('error', () => reject(new Error('websocket rejected')), { once: true }); }));
  return { ws, next };
}
async function mode(auth) {
  const label = auth ? 'auth on' : 'auth off';
  let upstream, child, sse, ws, clients = [], upstreamSocket;
  try {
    upstream = createServer(socket => { upstreamSocket = socketQueue(socket); });
    const upstreamPort = await listen(upstream), tcpPort = await freePort(), webPort = await freePort();
    const base = `http://127.0.0.1:${webPort}`;
    const authHeaders = auth ? { Authorization: basic } : {};
    child = spawn(binary, [], { env: { ...process.env, UPSTREAM_HOST: '127.0.0.1', UPSTREAM_PORT: String(upstreamPort), LISTEN_PORT: String(tcpPort), WEB_PORT: String(webPort), MAX_CLIENTS: '4', LOG_PACKETS: 'false', WEB_AUTH_ENABLED: String(auth), WEB_AUTH_USERNAME: credentials.username, WEB_AUTH_PASSWORD: credentials.password }, stdio: ['ignore', 'ignore', 'pipe'] });
    let stderr = ''; child.stderr.on('data', chunk => { stderr += chunk; });
    await check(`${label}: server readiness`, async () => {
      await waitFor(async () => { if (child.exitCode !== null) throw new Error(`server exited ${child.exitCode}: ${stderr}`); try { const r = await fetch(base + '/api/health'); return r.ok && upstreamSocket; } catch { return false; } });
    });
    const request = (path, options = {}) => fetch(base + path, { ...options, headers: { ...authHeaders, ...options.headers }, redirect: 'manual' });
    const post = (path, body, headers = {}) => request(path, { method: 'POST', headers: { 'Content-Type': 'application/json', ...headers }, body: typeof body === 'string' ? body : JSON.stringify(body) });
    const json = async (response, code = 200) => { assert.equal(response.status, code); assert.match(response.headers.get('content-type') || '', /^application\/json\b/); return response.json(); };
    await check(`${label}: static HTML, JavaScript, and public login`, async () => {
      const root = await request('/'); assert.equal(root.status, 200); assert.match(root.headers.get('content-type'), /^text\/html/); assert.match(await root.text(), /<html/i);
      const script = await request('/modules/packets.js'); assert.equal(script.status, 200); assert.match(script.headers.get('content-type'), /^(text|application)\/javascript/); assert.ok((await script.text()).length > 0);
      const login = await fetch(base + '/login.html'); assert.equal(login.status, 200); assert.match(login.headers.get('content-type'), /^text\/html/);
      if (auth) { const redirect = await fetch(base + '/', { redirect: 'manual' }); assert.equal(redirect.status, 302); assert.equal(redirect.headers.get('location'), 'login.html'); }
    });
    await check(`${label}: health JSON`, async () => {
      const x = await json(await fetch(base + '/api/health')); shape(x, { status: 'string', version: 'string', uptime: 'number', checks: 'object', timestamp: 'string' }); assert.equal(x.status, 'healthy');
      shape(x.checks, { upstream: 'object', clients: 'object', web_server: 'object' });
      shape(x.checks.upstream, { status: 'string', connected: 'boolean', address: 'string', last_connected: 'string' }); assert.equal(x.checks.upstream.connected, true); assert.equal(x.checks.upstream.status, 'healthy'); assert.equal(x.checks.upstream.address, `127.0.0.1:${upstreamPort}`);
      shape(x.checks.clients, { status: 'string', count: 'number', max: 'number' }); shape(x.checks.web_server, { status: 'string', port: 'number' }); assert.equal(x.checks.clients.max, 4); assert.equal(x.checks.web_server.port, webPort);
    });
    await check(`${label}: status JSON`, async () => { const x = await json(await request('/api/status')); status(x); assert.equal(x.upstream_addr, `127.0.0.1:${upstreamPort}`); assert.equal(x.listen_addr, `:${tcpPort}`); assert.equal(x.max_clients, 4); });
    await check(`${label}: public config excludes credentials`, async () => { const x = await json(await request('/api/config')); shape(x, { upstream_host: 'string', upstream_port: 'number', listen_port: 'number', max_clients: 'number', log_packets: 'boolean', web_port: 'number' }); assert.deepEqual(x, { upstream_host: '127.0.0.1', upstream_port: upstreamPort, listen_port: tcpPort, max_clients: 4, log_packets: false, web_port: webPort }); assert.ok(!JSON.stringify(x).includes(credentials.password)); });
    await check(`${label}: clients JSON`, async () => { const x = await json(await request('/api/clients')); shape(x, { clients: 'object', tcp_count: 'number', web_count: 'number', total_count: 'number', max_clients: 'number' }); assert.ok(Array.isArray(x.clients)); assert.equal(x.total_count, 0); assert.equal(x.max_clients, 4); });
    await check(`${label}: authentication and session lifecycle`, async () => {
      const initial = await json(await fetch(base + '/api/auth/check')); assert.deepEqual(initial, { authenticated: !auth, auth_enabled: auth });
      if (auth) {
        for (const path of ['/api/status', '/api/config', '/api/clients', '/api/events', '/api/ws']) assert.equal((await fetch(base + path)).status, 401, path);
        for (const path of ['/api/inject', '/api/clients/disconnect']) assert.equal((await fetch(base + path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })).status, 401, path);
        assert.equal((await request('/api/status')).status, 200);
        assert.deepEqual(await json(await request('/api/auth/check')), { authenticated: false, auth_enabled: true });
        assert.equal((await post('/api/login', { ...credentials, password: 'wrong' })).status, 401);
      }
      const login = await post('/api/login', credentials); assert.deepEqual(await json(login), { success: true });
      const cookieHeader = login.headers.get('set-cookie');
      if (auth) {
        assert.ok(cookieHeader); assert.match(cookieHeader, /HttpOnly/i); assert.match(cookieHeader, /SameSite=Lax/i);
        const cookie = cookieHeader.split(';')[0];
        assert.equal((await fetch(base + '/api/status', { headers: { Cookie: cookie } })).status, 200);
        assert.deepEqual(await json(await fetch(base + '/api/auth/check', { headers: { Cookie: cookie } })), { authenticated: true, auth_enabled: true });
        assert.deepEqual(await json(await fetch(base + '/api/logout', { method: 'POST', headers: { Cookie: cookie, 'Content-Type': 'application/json' }, body: '{}' })), { success: true });
        assert.equal((await fetch(base + '/api/status', { headers: { Cookie: cookie } })).status, 401);
      } else { assert.equal(cookieHeader, null); assert.deepEqual(await json(await post('/api/logout', {})), { success: true }); }
    });
    await check(`${label}: POST content type and invalid JSON`, async () => {
      for (const path of ['/api/inject', '/api/clients/disconnect']) {
        assert.equal((await request(path, { method: 'POST', headers: { 'Content-Type': 'text/plain' }, body: '{}' })).status, 415, path);
        assert.equal((await post(path, '{')).status, 400, path);
      }
      assert.equal((await request('/api/login', { method: 'POST', headers: { 'Content-Type': 'text/plain' }, body: '{}' })).status, 415);
      if (auth) assert.equal((await post('/api/login', '{')).status, 400);
    });
    await check(`${label}: TCP clients connect`, async () => {
      for (let i = 0; i < 2; i++) { const sock = createConnection({ host: '127.0.0.1', port: tcpPort }); clients.push(socketQueue(sock)); await timeout(new Promise((resolve, reject) => { sock.once('connect', resolve); sock.once('error', reject); })); }
      await waitFor(async () => (await (await request('/api/clients')).json()).tcp_count === 2);
      const x = await json(await request('/api/clients')); assert.equal(x.clients.length, 2); for (const c of x.clients) { shape(c, { id: 'string', addr: 'string', connected_at: 'string', type: 'string' }); assert.match(c.id, /^client#\d+$/); assert.equal(c.type, 'tcp'); }
    });
    await check(`${label}: inject upstream hex`, async () => { assert.deepEqual(await json(await post('/api/inject', { target: 'upstream', format: 'hex', data: '0x12 ab\n34' })), { success: true }); await upstreamSocket.take([0x12, 0xab, 0x34]); });
    await check(`${label}: inject downstream hex`, async () => { assert.deepEqual(await json(await post('/api/inject', { target: 'downstream', format: 'hex', data: '55 aa' })), { success: true }); await Promise.all(clients.map(c => c.take([0x55, 0xaa]))); });
    await check(`${label}: inject ASCII`, async () => { assert.deepEqual(await json(await post('/api/inject', { target: 'upstream', format: 'ascii', data: 'hello!' })), { success: true }); await upstreamSocket.take('hello!'); });
    await check(`${label}: inject invalid hex and target`, async () => { assert.equal((await post('/api/inject', { target: 'upstream', format: 'hex', data: 'zz' })).status, 400); assert.equal((await post('/api/inject', { target: 'elsewhere', format: 'ascii', data: 'x' })).status, 500); });
    await check(`${label}: SSE initial status and headers`, async () => {
      const controller = new AbortController(); const response = await request('/api/events', { signal: controller.signal }); assert.equal(response.status, 200); assert.match(response.headers.get('content-type'), /^text\/event-stream/); assert.equal(response.headers.get('access-control-allow-origin'), null);
      const reader = response.body.getReader(); let buffer = ''; const events = [];
      const pump = (async () => { try { for (;;) { const { done, value } = await reader.read(); if (done) break; buffer += new TextDecoder().decode(value); let i; while ((i = buffer.indexOf('\n\n')) >= 0) { const block = buffer.slice(0, i); buffer = buffer.slice(i + 2); const event = /^event: (.*)$/m.exec(block)?.[1]; const data = /^data: (.*)$/m.exec(block)?.[1]; if (event) events.push({ event, data }); } } } catch {} })();
      sse = { controller, pump, next: predicate => waitFor(() => { const i = events.findIndex(predicate); return i < 0 ? null : events.splice(i, 1)[0]; }) };
      const first = await sse.next(() => true); assert.equal(first.event, 'status'); status(JSON.parse(first.data));
    });
    await check(`${label}: WebSocket foreign origin rejected`, async () => {
      let rejected = false; try { const bad = await openWebSocket(`ws://127.0.0.1:${webPort}/api/ws`, { ...authHeaders, Origin: 'http://foreign.invalid' }); bad.ws.close(); } catch { rejected = true; } assert.equal(rejected, true);
    });
    await check(`${label}: WebSocket same origin initial status`, async () => {
      ws = await openWebSocket(`ws://127.0.0.1:${webPort}/api/ws`, { ...authHeaders, Origin: base }); const msg = await ws.next(() => true); shape(msg, { type: 'string', data: 'object' }); assert.equal(msg.type, 'status'); status(msg.data);
      const x = await json(await request('/api/clients')); assert.equal(x.tcp_count, 2); assert.equal(x.web_count, 2); assert.equal(x.total_count, 4); assert.equal(x.clients.filter(c => c.type === 'web').length, 1);
    });
    await check(`${label}: max clients includes SSE and WebSocket`, async () => {
      const extra = socketQueue(createConnection({ host: '127.0.0.1', port: tcpPort })); await extra.closed(); assert.equal((await request('/api/events')).status, 503);
    });
    await check(`${label}: upstream broadcast and packet log streams`, async () => {
      upstreamSocket.socket.write(Buffer.from([0xde, 0xad, 0x42])); await Promise.all(clients.map(c => c.take([0xde, 0xad, 0x42])));
      const pattern = /^\S+ \[PKT\] \[UP->\] de ad 42 \(3 bytes\)$/;
      const event = await sse.next(e => e.event === 'log' && pattern.test(e.data)); assert.match(event.data, pattern);
      const msg = await ws.next(m => m.type === 'log' && pattern.test(m.data.trimEnd())); shape(msg, { type: 'string', data: 'string' }); assert.match(msg.data.trimEnd(), pattern);
    });
    await check(`${label}: client upstream only and packet log streams`, async () => {
      clients[0].socket.write(Buffer.from([0xca, 0xfe, 0x51])); await upstreamSocket.take([0xca, 0xfe, 0x51]); await clients[1].quiet();
      const pattern = /^\S+ \[PKT\] \[->UP\] ca fe 51 \(3 bytes\) from client#\d+$/;
      const event = await sse.next(e => e.event === 'log' && pattern.test(e.data)); assert.match(event.data, pattern);
      const msg = await ws.next(m => m.type === 'log' && pattern.test(m.data.trimEnd())); shape(msg, { type: 'string', data: 'string' }); assert.match(msg.data.trimEnd(), pattern);
    });
    await check(`${label}: disconnect missing and unknown IDs`, async () => { assert.equal((await post('/api/clients/disconnect', {})).status, 400); assert.equal((await post('/api/clients/disconnect', { client_id: 'client#999999' })).status, 404); });
    await check(`${label}: disconnect real TCP client`, async () => {
      const localPort = clients[0].socket.localPort; const list = await json(await request('/api/clients')); const client = list.clients.find(c => c.type === 'tcp' && c.addr.endsWith(':' + localPort)); assert.ok(client);
      assert.deepEqual(await json(await post('/api/clients/disconnect', { client_id: client.id })), { success: true }); await clients[0].closed(); await waitFor(async () => (await (await request('/api/clients')).json()).tcp_count === 1);
    });
  } catch (err) { failed++; console.log(`FAIL ${label}: setup or unexpected error: ${err.stack || err}`); }
  finally {
    sse?.controller.abort(); ws?.ws.close(); for (const c of clients) c.socket.destroy(); upstreamSocket?.socket.destroy();
    if (child && child.exitCode === null) { child.kill('SIGKILL'); await timeout(new Promise(resolve => child.once('exit', resolve)), 3000).catch(() => {}); }
    if (upstream) await new Promise(resolve => upstream.close(resolve));
  }
}
await mode(false);
await mode(true);
console.log(`RESULT ${passed} passed, ${failed} failed`);
process.exitCode = failed ? 1 : 0;
