// Local compatibility patch for @tencent-weixin/openclaw-weixin 2.4.9.
// Usage: node scripts/patch-weixin-sender.mjs /absolute/weixin/package/root
// No network or personal configuration access. Restart/reload the plugin afterward.
import assert from 'node:assert/strict';
import {readFileSync, writeFileSync, copyFileSync, existsSync, renameSync} from 'node:fs';
import {join, isAbsolute} from 'node:path';

const root = process.argv[2];
assert.ok(root && isAbsolute(root), 'absolute Weixin package root required');
const pkg = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8'));
assert.equal(pkg.name, '@tencent-weixin/openclaw-weixin');
assert.equal(pkg.version, '2.4.9', 'review upstream implementation before patching another version');
const changes = [
 ['src/messaging/inbound.ts', [
  ['  From: string;\n', '  From: string;\n  SenderId: string;\n'],
  ['    From: from_user_id,\n', '    From: from_user_id,\n    SenderId: from_user_id,\n'],
 ]],
 ['dist/src/messaging/inbound.js', [
  ['        From: from_user_id,\n', '        From: from_user_id,\n        SenderId: from_user_id,\n'],
 ]],
].map(([relative, replacements]) => {
 const path = join(root, relative), original = readFileSync(path, 'utf8');
 let updated = original;
 for (const [before, after] of replacements) {
  if (updated.includes(after)) continue;
  assert.equal(updated.split(before).length, 2, `unexpected upstream layout: ${relative}`);
  updated = updated.replace(before, after);
 }
 return {path, original, updated};
});
// Validate every target before changing either source or runtime output.
for (const {path, original, updated} of changes) {
 if (original === updated) continue;
 const backup = path + '.atrium-sender-original';
 if (!existsSync(backup)) copyFileSync(path, backup);
 writeFileSync(path + '.atrium-tmp', updated);
 renameSync(path + '.atrium-tmp', path);
}
console.log('Weixin sender context patched; original files preserved beside each target.');
