// End-to-end smoke for H-406: real server + real web client + real Chrome.
// Pair → dashboard → slideshow → navigate/show/refresh via the admin CLI → remote key.
const { chromium } = require('playwright');
const { execSync } = require('child_process');
// Configuration via environment (see README.md):
//   ATRIUM_BIN  path to the atrium binary (default ../../bin/atrium)
//   ATRIUM_CFG  config.yaml of a running server with tls.mode: off
//   ATRIUM_URL  base URL (default http://127.0.0.1:18443)
//   E2E_OUT     directory for screenshots (default ./out)
const path = require('path');
const fs = require('fs');
const BIN = process.env.ATRIUM_BIN || path.resolve(__dirname, '../../bin/atrium');
const CFG = process.env.ATRIUM_CFG || path.resolve(__dirname, 'config.yaml');
const URL = process.env.ATRIUM_URL || 'http://127.0.0.1:18443';
const SCR = process.env.E2E_OUT || path.resolve(__dirname, 'out');
fs.mkdirSync(SCR, { recursive: true });
const cli = (args) => { try { return execSync(`${BIN} admin ${args} --config ${CFG}`, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }); } catch (e) { return `CLI_FAIL(${e.status}): ${e.stdout}${e.stderr}`; } };
const sleep = (ms) => new Promise(r => setTimeout(r, ms));
(async () => {
  const browser = await chromium.launch({ channel: 'chrome', headless: true });
  const ctx = await browser.newContext({ viewport: { width: 1920, height: 1080 } });
  const page = await ctx.newPage();
  const errors = [];
  page.on('console', m => { if (m.type() === 'error' || m.type() === 'warning') errors.push(`${m.type()}: ${m.text()}`); });
  page.on('response', r => { if (r.status() >= 400 && !r.url().includes('/pair/')) errors.push(`http ${r.status()} ${r.url()}`); });
  await page.goto(URL + '/');
  await page.waitForFunction(() => /\b\d{6}\b/.test(document.body.innerText), null, { timeout: 15000 });
  const code = await page.evaluate(() => document.body.innerText.match(/\b\d{6}\b/)[0]);
  console.log('[1] pair code shown:', code);
  await page.screenshot({ path: `${SCR}/01-pair.png` });
  console.log('[2] approve:', cli(`pair approve ${code} --id living_room_tv --name "Living room"`).trim());
  await page.waitForFunction(() => !/\b\d{6}\b/.test(document.body.innerText), null, { timeout: 20000 });
  await sleep(2000);
  console.log('[3] dashboard text:', (await page.evaluate(() => document.body.innerText)).replace(/\s+/g, ' ').slice(0, 220));
  await page.screenshot({ path: `${SCR}/02-dashboard.png` });
  console.log('[4] screens:', cli('screens list').trim().split('\n').slice(0, 3).join(' | '));
  // wait for previews
  let ready = 0;
  for (let i = 0; i < 40; i++) { const out = cli('sources list'); const m = out.match(/bound\S*\s+(\d+)\s+(\d+)/); ready = m ? +m[1] : 0; if (ready >= 12) break; await sleep(2000); }
  console.log('[5] photos ready:', ready);
  await sleep(4000);
  const imgs = await page.evaluate(() => Array.from(document.images).map(i => ({ src: i.src.replace(/.*\/media\//, ''), ok: i.complete && i.naturalWidth > 0 })));
  console.log('[6] images on dashboard:', JSON.stringify(imgs).slice(0, 300));
  await page.screenshot({ path: `${SCR}/03-slideshow.png` });
  console.log('[7] navigate:', cli('screen navigate living_room_tv --route photos --collection all --wait').trim().split('\n').pop());
  await sleep(1500); await page.screenshot({ path: `${SCR}/04-photos.png` });
  const list = JSON.parse(cli('photos list --collection all --limit 3 --json'));
  const items = list.items || list; const pid = items[0].id;
  console.log('[8] show', pid, ':', cli(`screen show living_room_tv --photo ${pid} --wait`).trim().split('\n').pop());
  await sleep(1500); await page.screenshot({ path: `${SCR}/05-photo.png` });
  console.log('[9] refresh:', cli('screen refresh living_room_tv --wait').trim().split('\n').pop());
  console.log('[10] navigate dashboard:', cli('screen navigate living_room_tv --route dashboard --wait').trim().split('\n').pop());
  console.log('[11] commands:', cli('commands list --screen living_room_tv').trim().split('\n').slice(0, 6).join('\n'));
  // remote key: Right arrow opens browser; Back returns
  await page.keyboard.press('ArrowRight'); await sleep(800);
  console.log('[12] after ArrowRight, text:', (await page.evaluate(() => document.body.innerText)).replace(/\s+/g, ' ').slice(0, 120));
  await page.keyboard.press('Escape'); await sleep(800);
  console.log('[13] browser errors:', errors.length ? errors.slice(0, 10).join('\n   ') : 'none');
  await browser.close();
  if (errors.some(e => !/401|favicon/.test(e))) { console.error('UNEXPECTED BROWSER ERRORS'); process.exit(2); }
})().catch(e => { console.error('SMOKE FAILED:', e.message); process.exit(1); });
