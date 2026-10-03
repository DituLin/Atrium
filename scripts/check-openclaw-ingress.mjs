// Explicit installed-runtime compatibility check. Does not contact IM services,
// invoke a model, load personal configuration, or write household records.
// Usage: node scripts/check-openclaw-ingress.mjs /absolute/path/to/openclaw
import assert from 'node:assert/strict';
import { readdirSync, mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, isAbsolute } from 'node:path';
import { pathToFileURL } from 'node:url';
import { registerPlugin, TOOL_FIELDS } from '../adapters/openclaw/index.js';

const root = process.argv[2];
assert.ok(root && isAbsolute(root), 'supply an absolute installed OpenClaw package path');
const dist = join(root, 'dist');
const files = readdirSync(dist).filter(name => /^message-hook-mappers-.*\.mjs$/.test(name));
assert.equal(files.length, 1, 'runtime mapper layout changed; review the installed dispatcher');
const exports = await import(pathToFileURL(join(dist, files[0])).href);
const byName = name => {
 const fn = Object.values(exports).find(value => typeof value === 'function' && value.name === name);
 assert.ok(fn, `missing runtime function: ${name}`);
 return fn;
};
const derive = byName('deriveInboundMessageHookContext');
const claimPair = byName('toPluginInboundClaimPair');
const directory = mkdtempSync(join(tmpdir(), 'atrium-ingress-compat-'));
let plugin;
try {
 const toolsFile = join(directory, 'tools.json');
 writeFileSync(toolsFile, JSON.stringify(Object.entries(TOOL_FIELDS).map(([name, fields]) => ({
  name, description: 'compatibility fixture', inputSchema: { type: 'object', additionalProperties: false,
   properties: Object.fromEntries(fields.map(field => [field, {type:'string'}])), required: [] },
 }))), {mode:0o600});
 const hooks = {}, claimed = new Set();
 let calls = 0;
 plugin = registerPlugin({
  pluginConfig: {agentId:'home', hostBinary:'/unused', hostArgs:[], toolsFile},
  on(name, handler) { hooks[name] = handler; }, registerTool() {}, registerService() {},
 }, {host: {async rpc(request) {
  assert.equal(request.method, 'ingress'); calls++;
  assert.match(request.run_id, /^[0-9a-f]{64}$/);
  const first = !claimed.has(request.run_id); claimed.add(request.run_id);
  return {claimed:first};
 }, close() {}}});
 function dispatch(raw) {
  const canonical = derive(raw), pair = claimPair(canonical);
  // Projection used by OpenClaw 2026.9.8 dispatch-from-config before_dispatch.
  return hooks.before_dispatch({messageId:canonical.messageId, content:canonical.content,
   channel:canonical.channelId, sessionKey:canonical.sessionKey, senderId:canonical.senderId}, {
   messageId:canonical.messageId, channelId:canonical.channelId, accountId:canonical.accountId,
   conversationId:pair.context.conversationId, sessionKey:canonical.sessionKey, senderId:canonical.senderId,
  });
 }
 for (const channel of ['discord', 'openclaw-weixin']) {
  const raw = {Provider:channel, Surface:channel, AccountId:'fixture',
   From:`${channel}:sender`, To:`${channel}:conversation`, SenderId:'fixture-sender',
   SessionKey:`agent:home:${channel}:direct:fixture`, Body:'synthetic read-only query',
   MessageSid:channel === 'discord' ? '123456789012345678' : 'openclaw-weixin-random-one',
   ...(channel === 'openclaw-weixin' ? {MessageSidFull:'provider-message-123'} : {}),
  };
  assert.equal(await dispatch(raw), undefined, `${channel}: first delivery must be admitted`);
  const repeated = {...raw, ...(channel === 'openclaw-weixin' ? {MessageSid:'openclaw-weixin-random-two'} : {})};
  assert.equal((await dispatch(repeated)).handled, true, `${channel}: native ID must survive dispatch-local ID changes`);
  const before = calls;
  assert.equal((await dispatch({...raw, SenderId:undefined})).handled, true);
  assert.equal(calls, before, 'missing sender must never claim a durable identity');
 }
 assert.equal(claimed.size, 2);
 console.log('PASS: installed OpenClaw mapper → Atrium ingress; canonical replay and missing identity checks');
 console.log('Synthetic contexts only; real IM transport and replies remain separately unverified.');
} finally {
 plugin?.close(); rmSync(directory, {recursive:true, force:true});
}
