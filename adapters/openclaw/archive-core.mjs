import { request } from 'node:https';
import { readFileSync, openSync, fstatSync, closeSync, constants } from 'node:fs';
import { createHash, randomUUID } from 'node:crypto';
const fail = code => Object.assign(new Error(`archive_${code}`), {code: `archive_${code}`});
const hash = v => createHash('sha256').update(v).digest('hex');
// Dedicated ingest transport. A changed credential cannot reassign pending sources.
export class ArchiveCore {
  constructor({origin, caFile, tokenFile, principal}) {
    const u = new URL(origin);
    if (u.protocol !== 'https:' || u.username || u.password || u.search || u.hash || u.pathname !== '/' || !principal) throw fail('config');
    this.origin = u.origin; this.ca = readFileSync(caFile); this.tokenFile = tokenFile;
    this.token = this.readToken(); this.tokenHash = hash(this.token);
    this.scope = JSON.stringify([this.origin, principal, this.tokenHash]);
  }
  readToken() {
    const fd = openSync(this.tokenFile, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const stat = fstatSync(fd);
      if (!stat.isFile() || (stat.mode & 0o777) !== 0o600 || stat.size > 4096) throw fail('credential');
      const token = readFileSync(fd, 'utf8').trim();
      if (!token || /\s/.test(token)) throw fail('credential');
      return token;
    } finally { closeSync(fd); }
  }
  async send(path, body, type, signal) {
    if (hash(this.readToken()) !== this.tokenHash) throw fail('credential');
    return new Promise((resolve, reject) => {
      const req = request(this.origin + path, {
        method: 'POST', ca: this.ca,
        signal: AbortSignal.any([AbortSignal.timeout(120000), ...(signal ? [signal] : [])]),
        headers: {authorization: `Bearer ${this.token}`, 'content-type': type, 'content-length': body.length},
      }, res => {
        const chunks = []; let size = 0;
        res.on('data', b => { size += b.length; if (size > 65536) { res.destroy(); reject(fail('receipt')); } else chunks.push(b); });
        res.on('error', () => reject(fail('retry')));
        res.on('end', () => {
          if (res.statusCode === 404) { let missing=false;try{missing=path.endsWith('/lookup')&&JSON.parse(Buffer.concat(chunks))?.error?.code==='not_found';}catch{}if(missing)resolve(null);else reject(fail('retry'));return; }
          if ([401,403].includes(res.statusCode)) { reject(fail('forbidden')); return; }
          if (res.statusCode === 409) { reject(fail('conflict')); return; }
          if (res.statusCode !== 200) { reject(fail('retry')); return; }
          try {
            const value = JSON.parse(Buffer.concat(chunks));
            if (value.availability !== 'available' || !value.data) throw fail('receipt');
            resolve(value.data);
          } catch { reject(fail('receipt')); }
        });
      });
      req.on('error', () => reject(fail('retry'))); req.end(body);
    });
  }
  lookup(source, signal) {
    return this.send('/api/v1/integrations/archives/intakes/lookup', Buffer.from(JSON.stringify(source)), 'application/json', signal);
  }
  upload(metadata, bytes, signal) {
    const boundary = `atrium-${randomUUID()}`;
    const body = Buffer.concat([
      Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="metadata"\r\nContent-Type: application/json\r\n\r\n${JSON.stringify(metadata)}\r\n--${boundary}\r\nContent-Disposition: form-data; name="content"; filename="original"\r\nContent-Type: application/octet-stream\r\n\r\n`),
      bytes, Buffer.from(`\r\n--${boundary}--\r\n`),
    ]);
    return this.send('/api/v1/integrations/archives/intakes', body, `multipart/form-data; boundary=${boundary}`, signal);
  }
}
