import { spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { StringDecoder } from 'node:string_decoder';

const fail = code => Object.assign(new Error(code), { code });
// Protocol data is never logged; stderr belongs to the host's sanitized logger.
export class Host {
  constructor(config, { timeoutMs = 30000, spawnHost = spawn, maxPending = 64 } = {}) {
    if (!Number.isInteger(maxPending) || maxPending < 1 || maxPending > 64) throw fail('invalid_capacity');
    this.maxPending = maxPending; this.onCapacity = () => {};
    this.config = config; this.timeoutMs = timeoutMs; this.spawnHost = spawnHost;
    this.onIdle = () => {};
    this.pending = new Map(); this.child = null; this.closed = false; this.onCrash = () => {};
  }
  ensure() {
    if (this.closed) throw fail('host_closed');
    if (this.child) return this.child;
    const child = this.spawnHost(this.config.hostBinary, this.config.hostArgs, { stdio: ['pipe', 'pipe', 'inherit'], shell: false });
    this.child = child; let buffer = ''; const decoder = new StringDecoder('utf8');
    const die = () => {
      if (this.child !== child) return;
      this.child = null;
      for (const entry of this.pending.values()) entry.reject(fail('host_unavailable'));
      this.pending.clear(); this.onCrash(); child.kill();
    };
    child.on('error', die); child.on('exit', die); child.stdin.on('error', die);
    child.stdout.on('data', chunk => {
      buffer += decoder.write(chunk);
      if (Buffer.byteLength(buffer) > 1024 * 1024) { die(); return; }
      let end;
      while ((end = buffer.indexOf('\n')) >= 0) {
        const line = buffer.slice(0, end); buffer = buffer.slice(end + 1);
        let reply;
        try { reply = JSON.parse(line); } catch { die(); return; }
        if (!reply || typeof reply.id !== 'string') { die(); return; }
        const entry = this.pending.get(reply.id); if (!entry) continue;
        if (reply.error) {
          const error = fail('host_error');
          for (const key of ['operation_id', 'command_id']) if (typeof reply.error[key] === 'string' && /^[0-7][0-9A-HJKMNP-TV-Z]{25}$/.test(reply.error[key])) error[key] = reply.error[key];
          entry.reject(error);
        } else entry.resolve(reply.result);
      }
    });
    return child;
  }
  rpc(request, signal) {
    if (!this.closed && this.pending.size >= this.maxPending) {
      this.close(); this.onCapacity(); return Promise.reject(fail('capacity_exceeded'));
    }
    if (signal?.aborted) {
      if (request.run_id && this.child && request.method !== 'cancel') this.rpc({ method: 'cancel', run_id: request.run_id }).catch(() => {});
      return Promise.reject(fail('cancelled'));
    }
    let child;
    try { child = this.ensure(); } catch { return Promise.reject(fail('host_unavailable')); }
    const id = randomUUID();
    return new Promise((resolve, reject) => {
      const cleanup = () => { clearTimeout(timer); signal?.removeEventListener('abort', abort); this.pending.delete(id); if (this.pending.size === 0) queueMicrotask(() => this.onIdle()); };
      const settle = fn => value => { cleanup(); fn(value); };
      const abort = () => {
        // Cancellation closes the entire run. It never retries the action.
        if (request.run_id && this.child === child && request.method !== 'cancel') this.rpc({ method: 'cancel', run_id: request.run_id }).catch(() => {});
        entry.reject(fail('cancelled'));
      };
      const entry = { resolve: settle(resolve), reject: settle(reject) };
      const timer = setTimeout(() => { abort(); }, this.timeoutMs);
      this.pending.set(id, entry); signal?.addEventListener('abort', abort, { once: true });
      child.stdin.write(JSON.stringify({ ...request, id }) + '\n', error => { if (error) entry.reject(fail('host_unavailable')); });
      if (signal?.aborted) abort();
    });
  }
  releaseIdle() {
    if (this.closed || this.pending.size !== 0) return false;
    const child = this.child; this.child = null;
    if (!child) return true;
    // Detach first: exit/error callbacks from this generation cannot affect a
    // future child or close new runs. EOF plus SIGTERM releases CLI handles.
    child.stdin.end(); child.kill();
    return true;
  }
  close() {
    this.closed = true;
    for (const entry of this.pending.values()) entry.reject(fail('host_closed'));
    this.pending.clear(); const child = this.child; this.child = null; child?.kill();
  }
}
