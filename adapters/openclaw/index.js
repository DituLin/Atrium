import { readFileSync, openSync, closeSync, fstatSync, constants } from 'node:fs';
import { isAbsolute } from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { Host } from './host.js';

export const TOOL_FIELDS = Object.freeze({
 home_get_status: [], home_list_screens: [], home_get_screen: ['screen_id'],
 home_get_nas_status: [], home_list_photos: ['collection', 'limit', 'cursor'],
 home_get_photo: ['photo_id'], home_get_command: ['command_id'],
 home_navigate_screen: ['screen_id', 'route', 'collection'],
 home_show_photo: ['screen_id', 'photo_id'], home_refresh_screen: ['screen_id'],
});
const names = Object.keys(TOOL_FIELDS);
const hash = values => createHash('sha256').update(JSON.stringify(values)).digest('hex');
const opaque = value => typeof value === 'string' && value.length > 0 && value.length <= 512 && !/[\x00-\x1f\x7f]/.test(value);
const object = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const denied = (error) => {
 let text = '家庭工具无法确认本轮授权或执行状态，请勿重复发送屏幕动作。';
 if (typeof error?.operation_id === 'string' && /^[0-7][0-9A-HJKMNP-TV-Z]{25}$/.test(error.operation_id)) text += ' 原操作：' + error.operation_id + '。';
 if (typeof error?.command_id === 'string' && /^[0-7][0-9A-HJKMNP-TV-Z]{25}$/.test(error.command_id)) text += ' 原命令：' + error.command_id + '。';
 return { isError: true, content: [{ type: 'text', text }] };
};
function identity(ctx) { return opaque(ctx?.agentId) && opaque(ctx.sessionId || ctx.sessionKey) ? [ctx.agentId, ctx.sessionId || ctx.sessionKey] : null; }
function eventId(event, ctx, key) {
 const a = event?.[key], b = ctx?.[key];
 if (a !== undefined && b !== undefined && a !== b) return null;
 const value = a ?? b; return opaque(value) ? value : null;
}
function loadTools(path) {
 const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
 let tools;
 try {
  const stat = fstatSync(fd);
  if (!stat.isFile() || (stat.mode & 0o077) !== 0 || stat.size > 256 * 1024) throw new Error('invalid_tools_file');
  tools = JSON.parse(readFileSync(fd, 'utf8'));
 } finally { closeSync(fd); }
 if (!Array.isArray(tools) || tools.length !== names.length) throw new Error('invalid_tools_inventory');
 const seen = new Set();
 for (const tool of tools) {
  const schema = tool?.inputSchema, name = tool?.name;
  if (!Object.hasOwn(TOOL_FIELDS, name) || seen.has(name) || !object(schema) || schema.type !== 'object' || schema.additionalProperties !== false || !object(schema.properties)) throw new Error('invalid_tools_inventory');
  if (Object.keys(schema.properties).some(key => !TOOL_FIELDS[name].includes(key)) || /"(?:operation_id|wait_ms)"/.test(JSON.stringify(schema))) throw new Error('invalid_tools_schema');
  if (typeof tool.description !== 'string' || !Array.isArray(schema.required) || schema.required.some(key => !Object.hasOwn(schema.properties, key))) throw new Error('invalid_tools_schema');
  seen.add(name);
 }
 return tools;
}
function validateConfig(config) {
 if (!object(config) || Object.keys(config).some(key => !['agentId','hostBinary','hostArgs','toolsFile'].includes(key)) || !opaque(config.agentId) || config.agentId.length > 128 || !isAbsolute(config.hostBinary ?? '') || !isAbsolute(config.toolsFile ?? '') || !Array.isArray(config.hostArgs) || config.hostArgs.length > 32 || config.hostArgs.some(v => typeof v !== 'string' || v.includes('\0'))) throw new Error('invalid_atrium_config');
 return Object.freeze({agentId:config.agentId,hostBinary:config.hostBinary,hostArgs:Object.freeze([...config.hostArgs]),toolsFile:config.toolsFile});
}
function createController(api, options, config, tools) {
 const host = options.host ?? new Host(config, options);
 const diagnosticInstance = hash([randomUUID()]);
 function diagnostic(stage, ctx, event = {}, extra = {}) {
  const session = ctx?.sessionId || ctx?.sessionKey;
  const rawRun = eventId(event, ctx, 'runId'), rawCall = eventId(event, ctx, 'toolCallId');
  api.logger?.info?.('atrium_identity ' + JSON.stringify({stage, instance_hash: diagnosticInstance,
   agent_present: opaque(ctx?.agentId), session_id_present: opaque(ctx?.sessionId), session_key_present: opaque(ctx?.sessionKey),
   session_hash: opaque(session) ? hash([session]) : '',
   run_hash: rawRun ? hash([rawRun]) : '', call_hash: rawCall ? hash([rawCall]) : '', ...extra}));
 }
 const runs = new Map(), calls = new Map();
 let disabled = false;
 const maxRuns = options.maxRuns ?? 4096, maxCalls = options.maxCalls ?? 32768;
 if (!Number.isInteger(maxRuns) || maxRuns < 1 || maxRuns > 4096 || !Number.isInteger(maxCalls) || maxCalls < 1 || maxCalls > 32768) throw new Error('invalid_capacity');
 const terminal = run => { run.active = false; run.closed = true; clearTimeout(run.timer); };
 function disableForCapacity(closeHost = true) {
  if (disabled) return;
  disabled = true; for (const run of runs.values()) terminal(run); if (closeHost) host.close();
  api.logger?.warn?.('atrium_capacity_exceeded: restart the plugin lifecycle before new household work');
 }
 function releaseIdle() {
  if (!disabled && [...runs.values()].every(run => run.closed && !run.ending)) host.releaseIdle?.();
 }
 host.onIdle = releaseIdle;
 host.onCapacity = () => disableForCapacity();
 host.onCrash = () => { for (const run of runs.values()) terminal(run); };
 function scope(ctx) { const id = identity(ctx); return !disabled && id && id[0] === config.agentId ? id : null; }
 function lookup(event, ctx) { const id = scope(ctx), runId = eventId(event, ctx, 'runId'); return id && runId ? { id, key: hash([...id, runId]), runId } : null; }
 api.on('before_agent_start', async (event, ctx) => {
  const info = lookup(event, ctx); diagnostic('start', ctx, event, {scope_valid: !!info}); if (!info || runs.has(info.key)) return;
  if (runs.size >= maxRuns) { disableForCapacity(); return; }
  const run = { active: false, key: info.key }; runs.set(info.key, run);
  run.timer = setTimeout(() => { terminal(run); run.ending = true; host.rpc({ method: 'cancel', run_id: run.key }).catch(() => {}).finally(() => { run.ending = false; releaseIdle(); }); }, options.runTimeoutMs ?? 30000);
  try { await host.rpc({ method: 'start', run_id: run.key }); if (!run.closed) run.active = true; } catch { terminal(run); releaseIdle(); }
 });
 api.on('before_tool_call', async (event, ctx) => {
  if (!names.includes(event?.toolName)) return;
  const info = lookup(event, ctx), callId = eventId(event, ctx, 'toolCallId');
  const run = info && runs.get(info.key);
  diagnostic('before_tool', ctx, event, {scope_valid: !!info, run_found: !!run, run_active: !!run?.active});
  if (!run?.active || !callId) return { block: true, blockReason: 'atrium_run_unavailable' };
  const key = hash([...info.id, callId]);
  const prior = calls.get(key);
  if (prior && (prior.ambiguous || prior.run !== run || prior.tool !== event.toolName)) {
   prior.ambiguous = true;
   return { block: true, blockReason: 'atrium_call_conflict' };
  }
  if (!prior && calls.size >= maxCalls) { disableForCapacity(); return { block: true, blockReason: 'atrium_capacity_exceeded' }; }
  calls.set(key, { run, tool: event.toolName, wire: hash([...info.id, info.runId, callId]) });
 });
 api.on('agent_end', async (event, ctx) => {
  const info = lookup(event, ctx); if (!info) return;
  let run = runs.get(info.key);
  if (run?.closed) return;
  // End may precede start. Persist a tombstone even when this needs a fresh
  // host process: it is a lifecycle closure, never an action or run restart.
  if (!run && runs.size >= maxRuns) {
   // Refuse new work immediately, but still attempt the authoritative end
   // tombstone without allocating another historical map entry.
   disableForCapacity(false);
   try { await host.rpc({ method: 'end', run_id: info.key }); } catch { /* lifecycle remains disabled */ } finally { host.close(); }
   return;
  }
  if (!run) { run = { active: false, key: info.key }; runs.set(info.key, run); }
  terminal(run); run.ending = true;
  try {
   const result = await host.rpc({ method: 'end', run_id: run.key });
   if (result?.closed !== true) throw new Error('end_unconfirmed');
   run.ending = false; releaseIdle();
  } catch {
   // A failed tombstone must not silently leave future starts available.
   disabled = true; for (const other of runs.values()) terminal(other); host.close();
  }
 });
 api.registerTool(ctx => {
  const id = scope(ctx); diagnostic('factory', ctx, {}, {scope_valid: !!id}); if (!id) return null;
  return tools.map(tool => ({ name: tool.name, label: tool.name, description: tool.description, parameters: structuredClone(tool.inputSchema),
   async execute(callId, params, signal) {
    const mapping = opaque(callId) && calls.get(hash([...id, callId]));
    const invalidParams = !object(params) || Object.keys(params).some(key => !TOOL_FIELDS[tool.name].includes(key));
    diagnostic('execute', ctx, {toolCallId: callId}, {disabled, mapping_found: !!mapping, mapping_ambiguous: !!mapping?.ambiguous, run_active: !!mapping?.run?.active, tool_matches: mapping?.tool === tool.name, params_valid: !invalidParams});
    if (disabled || !mapping || mapping.ambiguous || !mapping.run.active || mapping.tool !== tool.name || invalidParams) return denied();
    try {
     const response = await host.rpc({ method: 'call', run_id: mapping.run.key, call_id: mapping.wire, tool: tool.name, arguments: params }, signal);
     const result = response?.tool_result;
     if (!object(result) || !Array.isArray(result.content) || result.content.some(item => item?.type !== 'text' || typeof item.text !== 'string')) { diagnostic('response_invalid', ctx, {toolCallId: callId}); return denied(); }
     const content = result.content.map(item => ({ type: 'text', text: item.text }));
     if (typeof response.report?.TextZH === 'string') content.push({ type: 'text', text: response.report.TextZH });
     return { content, ...(result.isError ? { isError: true } : {}) };
    } catch (error) { diagnostic('host_failure', ctx, {toolCallId: callId}); terminal(mapping.run); releaseIdle(); return denied(error); }
   }
  }));
 }, { names, optional: true });
 const plugin = { close() { disabled = true; for (const run of runs.values()) terminal(run); host.close(); } };
 api.registerService?.({ id: 'atrium-home-host', start() {}, stop() { plugin.close(); } });
 return plugin;
}
// OpenClaw may build independent hook and tool registries in one process.
// Share only identical maintainer configuration + validated inventory, never
// session/model parameters. Tests opt into sharing with an explicit Map.
const registrySymbol = Symbol.for('io.atrium.openclaw.controllers.v1');
export function registerPlugin(api, options = {}) {
 const config = validateConfig(api.pluginConfig);
 let inventory;
 try { inventory = loadTools(config?.toolsFile); } catch { throw new Error('invalid_tools_file_or_inventory'); }
 const key = hash([config?.agentId, config?.hostBinary, config?.hostArgs, config?.toolsFile, inventory]);
 const injected = Object.keys(options).length > 0;
 const registry = options.sharedRegistry ?? (injected ? new Map() : (globalThis[registrySymbol] ??= new Map()));
 if (!(registry instanceof Map)) throw new Error('invalid_controller_registry');
 let state = registry.get(key);
 if (!state) {
  if (registry.size >= 32) throw new Error('atrium_controller_capacity_restart_required');
  const hooks = [], factories = [];
  const proxy = { get pluginConfig() { return api.pluginConfig; }, get logger() { return api.logger; },
   on(name, handler) { hooks.push([name, handler]); },
   registerTool(factory, toolOptions) { factories.push([factory, toolOptions]); }, registerService() {} };
  const controller = createController(proxy, options, config, inventory);
  state = { hooks, factories, controller, refs: 0 }; registry.set(key, state);
 }
 if (state.refs >= 128) throw new Error('atrium_registration_capacity_restart_required');
 state.refs++; let closed = false;
 const attachment = { close() {
  if (closed) return; closed = true; state.refs--;
  if (state.refs === 0) { state.controller.close(); if (registry.get(key) === state) registry.delete(key); }
 } };
 try {
  for (const [name, handler] of state.hooks) api.on(name, (event, ctx) => {
   if (closed) return undefined; // A stale hook cannot veto a still-live registry; its own tools remain closed.
   return handler(event, ctx);
  });
  for (const [factory, toolOptions] of state.factories) api.registerTool(ctx => {
   if (closed) return null;
   const tools = factory(ctx); if (!tools) return tools;
   return tools.map(tool => ({...tool, execute(...args) { return closed ? Promise.resolve(denied()) : tool.execute(...args); }}));
  }, toolOptions);
  api.registerService?.({id:'atrium-home-host', start() {}, stop() { attachment.close(); }});
 } catch { attachment.close(); throw new Error('atrium_registration_failed'); }
 return attachment;
}
export default { id: 'atrium-home', name: 'Atrium Home', description: 'Restricted household tools through the durable Atrium Brain host', register(api) { registerPlugin(api); } };
