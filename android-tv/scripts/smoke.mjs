// Real-device test. Requires a paired DEBUG APK in the foreground, Node 24+,
// ADB and an admin CLI on the host. Never prints credentials or photo content.
import {execFileSync} from 'node:child_process';
import assert from 'node:assert/strict';
const adb=process.env.ADB || 'adb';
const binary=process.env.ATRIUM_BINARY || 'atrium';
const config=process.env.ATRIUM_CONFIG;
const screen=process.env.ATRIUM_SCREEN;
const device=process.env.ATRIUM_DEVICE;
assert(config && screen,'Set ATRIUM_CONFIG and ATRIUM_SCREEN');
if(!device){
 const connected=execFileSync(adb,['devices'],{encoding:'utf8',timeout:10000}).split('\n').filter(line=>/\tdevice\s*$/.test(line));
 assert.equal(connected.length,1,'Set ATRIUM_DEVICE when ADB has zero or multiple ready devices');
}
function shell(...args){return execFileSync(adb,[...(device?['-s',device]:[]),...args],{encoding:'utf8',timeout:20000});}
if(process.env.ATRIUM_TEST_NETWORK==='1')assert(shell('get-devpath').trim().startsWith('usb:'),'Network test requires USB ADB so Wi-Fi can be restored');
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
let restoreWifi=false, failScript=false, injectedScriptFailure=false;
let ws,id=0;const pending=new Map();
async function attach(){
 ws?.close();
 const pid=shell('shell','pidof','io.atrium.tv').trim();assert(pid,'TV app not running');
 shell('forward','tcp:9223',`localabstract:webview_devtools_remote_${pid}`);
 let pages;
 for(let n=0;n<30;n++){try{pages=await(await fetch('http://127.0.0.1:9223/json',{signal:AbortSignal.timeout(3000)})).json();if(pages.length)break;}catch{}await sleep(500);}
 assert(pages?.length,'WebView debug endpoint absent');
 ws=new WebSocket(pages[0].webSocketDebuggerUrl);await new Promise((r,j)=>{ws.onopen=r;ws.onerror=j});
 ws.onmessage=async e=>{const m=JSON.parse(e.data);if(m.id){pending.get(m.id)?.(m);pending.delete(m.id);}
  if(m.method==='Fetch.requestPaused') {
   if(failScript){failScript=false;injectedScriptFailure=true;await call('Fetch.fulfillRequest',{requestId:m.params.requestId,responseCode:503,responseHeaders:[{name:'Content-Type',value:'text/javascript'}],body:''});await call('Fetch.disable');}
   else await call('Fetch.continueRequest',{requestId:m.params.requestId});
  }
 };
}
function call(method,params={}){return new Promise((resolve,reject)=>{const i=++id;const timer=setTimeout(()=>{pending.delete(i);reject(new Error(`CDP timeout: ${method}`));},10000);pending.set(i,m=>{clearTimeout(timer);if(m.error)reject(new Error(`CDP ${method}: ${m.error.message}`));else resolve(m)});ws.send(JSON.stringify({id:i,method,params}));});}
async function evaluate(expression){const r=await call('Runtime.evaluate',{expression,returnByValue:true});assert(!r.result?.exceptionDetails,'JS evaluation failed');return r.result.result.value;}
async function until(expression,label,ms=20000){const start=Date.now();while(Date.now()-start<ms){try{if(await evaluate(expression))return;}catch{}await sleep(200);}throw new Error(`Timeout: ${label}`);}
async function waitForScreen(){const start=Date.now();while(Date.now()-start<30000){const value=JSON.parse(execFileSync(binary,['admin','screens','get',screen,'--config',config,'--json'],{encoding:'utf8',timeout:5000}));if(value.online===true)return;await sleep(500);}throw new Error('Screen did not come online before command setup');}
function command(kind,...args){const start=Date.now();const out=execFileSync(binary,['admin','screen',kind,screen,...args,'--wait','--config',config,'--json'],{encoding:'utf8',timeout:25000});const result=JSON.parse(out);assert.equal(result.status,'applied',`Command ${kind} failed: ${result.error_code||result.status}`);console.log(JSON.stringify({command:kind,elapsed_ms:Date.now()-start,core_resolved_ms:result.resolved_at?Date.parse(result.resolved_at)-Date.parse(result.issued_at):null,status:result.status}));return result;}
async function key(n){shell('shell','input','keyevent',String(n));await sleep(250);}
const decoded="!!document.querySelector('.viewer__image:not(.viewer__image--retained)')?.naturalWidth && !document.querySelector('.viewer__status')";
const onlineHome="!!document.querySelector('.dashboard') && !!document.querySelector('.home__status')?.innerText.includes('家庭服务已连接')";
const focusedPhoto="document.activeElement?.closest('.thumb')?.querySelector('img')?.getAttribute('src')";
async function operations(){
 await key(23);await until("document.activeElement?.innerText==='关闭操作'",'operations initial focus');
 shell('shell','input','keyevent','--longpress','23');await sleep(250);
 // One fresh press closes the operations. Its repeat must not reopen them.
 assert.equal(await evaluate("!!document.querySelector('.viewer__operations')"),false,'held Enter reopened operations');
 await key(23);await until("!!document.querySelector('.viewer__operations')",'operations reopened');
 await key(4);assert(await evaluate("!!document.querySelector('.viewer') && !document.querySelector('.viewer__operations')"),'Back consumed more than one layer');
}
async function songFlow(){
 await until("!!document.querySelector('.dashboard')",'home page before key flow');
 if(await evaluate("document.activeElement?.classList.contains('home__status')"))await key(21);
 else await key(19);
 await until("document.activeElement?.getAttribute('aria-label')==='打开当前照片'",'home photo focus');
 await until("!!document.querySelector('.slide--in')?.naturalWidth",'home photo decoded before confirmation');
 await key(23);await until(decoded,'home photo decode');await operations();await key(4);
 await until("!!document.querySelector('.dashboard')",'home viewer return');
 await key(20);await key(22);
 assert(await evaluate("!!document.querySelector('.dashboard') && document.activeElement?.getAttribute('data-nav')==='photos'"),'navigation focus changed the route');
 await key(23);
 const labels=['最近新增','今天拍摄','随心看看','全部照片'];
 let photo=null,scrollChecks=0,nextPreviousChecks=0;
 for(let index=0;index<labels.length;index++){
  const label=JSON.stringify(labels[index]);
  await until(`document.activeElement?.getAttribute('role')==='tab' && document.activeElement?.innerText===${label}`,'collection tab focus');
  await key(23);
  await until(`document.querySelector('[role=tab][aria-selected=true]')?.innerText===${label} && !!document.activeElement?.closest('.photogrid,.photos__recovery')`,'confirmed collection content');
  assert(!await evaluate("document.querySelector('.photos__empty')?.innerText.includes('暂时无法加载')"),'collection request failed');
  const count=await evaluate("document.querySelectorAll('.thumb').length");
  let downSteps=0,restoredScroll=null;
  if(count){
   if(count>1)await key(22);
   const columns=await evaluate("getComputedStyle(document.querySelector('.photogrid')).gridTemplateColumns.split(' ').length");
   downSteps=Math.max(0,Math.min(4,Math.floor((count-3)/columns)));
   for(let row=0;row<downSteps;row++)await key(20);
   const origin=await evaluate(focusedPhoto);assert(origin,'focused card has no photo');
   photo??=origin.match(/photos\/([^?]+)/)?.[1];
   const scroll=await evaluate("document.querySelector('.photogrid').scrollTop");
   restoredScroll=scroll;
   await key(23);await until(decoded,'collection photo decode');
   if(count>2){
    const current=await evaluate("document.querySelector('.viewer__image:not(.viewer__image--retained)').getAttribute('src')");
    await key(22);await until(`${decoded} && document.querySelector('.viewer__image:not(.viewer__image--retained)').getAttribute('src')!==${JSON.stringify(current)}`,'next photo');
    await key(21);await until(`${decoded} && document.querySelector('.viewer__image:not(.viewer__image--retained)').getAttribute('src')===${JSON.stringify(current)}`,'previous photo');
    nextPreviousChecks++;
   }
   await key(4);await until(`${focusedPhoto}===${JSON.stringify(origin)}`,'source card restored');
   assert(Math.abs(await evaluate("document.querySelector('.photogrid').scrollTop")-scroll)<=1,'gallery scroll not restored');
   if(scroll>0)scrollChecks++;
  }
  console.log(JSON.stringify({check:'remote collection roundtrip',collection:labels[index],loaded:count,scroll:restoredScroll,previous_next:count>2?'PASS':'NOT_APPLICABLE',result:'PASS'}));
  for(let row=0;row<=downSteps;row++)await key(19);
  if(index<3)await key(22);
 }
 assert(scrollChecks>0,'Insufficient fixture: no nonzero gallery scroll roundtrip verified');
 assert(nextPreviousChecks>0,'Insufficient fixture: no previous/next roundtrip verified');
 await key(19);await key(22);await key(23);
 await until("!!document.querySelector('.screen--settings')",'settings via remote');
 const settingsTabs=['连接状态','照片来源','关于 Atrium'];
 for(let index=0;index<settingsTabs.length;index++){
  const label=JSON.stringify(settingsTabs[index]);
  await until(`document.activeElement?.id==='settings-tab-${index}'`,'settings tab focus');
  if(index)assert(await evaluate(`document.querySelector('.settings__panel h2')?.innerText!==${label}`),'settings focus changed selected panel');
  await key(23);
  await until(`document.querySelector('.settings__panel h2')?.innerText===${label}`,'settings tab confirmed');
  await key(22);await until("!!document.activeElement?.closest('.settings__recovery')",'settings recovery focus');
  await key(23);
  await until("document.querySelector('.settings__recovery [role=status]')?.innerText==='已更新状态'",'settings authentic refresh');
  assert(await evaluate("!!document.activeElement?.closest('.settings__recovery')"),'settings refresh stole focus');
  await key(21);if(index<2)await key(20);
 }
 command('refresh');
 assert(await evaluate("!!document.querySelector('.screen--settings')"),'refresh changed settings route');
 await key(4);await until("!!document.querySelector('.screen--photos')",'settings returns to photos');
 await until("document.activeElement?.getAttribute('data-nav')==='settings'",'settings source focus restored');
 await key(4);await until("!!document.querySelector('.dashboard')",'photos return home');
 // Returning home may restore the originating navigation item. Back first
 // returns focus to the photo; only a further Back may open native controls.
 if(!await evaluate("document.activeElement?.getAttribute('aria-label')==='打开当前照片'"))await key(4);
 await key(4);shell('shell','uiautomator','dump','/sdcard/atrium-smoke.xml');
 const menu=shell('shell','cat','/sdcard/atrium-smoke.xml');
 assert(menu.includes('继续展示')&&menu.includes('退出应用'),'root Back did not open native menu');
 await key(23);shell('shell','uiautomator','dump','/sdcard/atrium-smoke.xml');
 assert(!shell('shell','cat','/sdcard/atrium-smoke.xml').includes('退出应用'),'native Continue did not close menu');
 await until("document.activeElement?.getAttribute('aria-label')==='打开当前照片'",'native Continue returns to page');
 assert(photo,'No collection had a displayable photo');return photo;
}
try {
 shell('shell','am','start','-W','-n','io.atrium.tv/.MainActivity');
 await attach();
 await waitForScreen();
 command('navigate','--route','dashboard');
 await until("!!document.querySelector('.dashboard')",'paired dashboard');
 if(process.env.ATRIUM_TEST_BOOT==='1') {
  await call('Network.setCacheDisabled',{cacheDisabled:true});failScript=true;
  await call('Fetch.enable',{patterns:[{urlPattern:'*.js',requestStage:'Request'}]});
  await call('Page.reload',{ignoreCache:true});
  await until("!!document.getElementById('boot')",'boot placeholder after failed JS');
  const started=Date.now();
  await until("!!document.querySelector('.dashboard')",'automatic startup-resource recovery',25000);
  assert(injectedScriptFailure,'script failure not injected');
  console.log(JSON.stringify({check:'HTML succeeds, JavaScript 503, automatic recovery',elapsed_ms:Date.now()-started,result:'PASS'}));
 }
 assert(await evaluate('innerWidth===document.documentElement.scrollWidth'),'viewport overflow');
 const photo=await songFlow();
 command('show','--photo',photo);
 await until(decoded,'show image decoded');
 command('refresh');
 assert(await evaluate("!!document.querySelector('.viewer')"),'refresh changed route');
 command('navigate','--route','dashboard');
 await until("!!document.querySelector('.dashboard')",'dashboard restored');
 await key(3);shell('shell','am','start','-n','io.atrium.tv/.MainActivity');
 await until(onlineHome,'foreground recovery');
 shell('shell','am','force-stop','io.atrium.tv');shell('shell','am','start','-n','io.atrium.tv/.MainActivity');
 await attach();await until("!!document.querySelector('.dashboard')",'process restart pairing persistence');
 if(process.env.ATRIUM_TEST_NETWORK==='1') {
  assert.equal(shell('shell','settings','get','global','wifi_on').trim(),'1','Network test requires Wi-Fi initially on');
  restoreWifi=true;shell('shell','svc','wifi','disable');await sleep(1500);
  shell('shell','am','force-stop','io.atrium.tv');shell('shell','am','start','-n','io.atrium.tv/.MainActivity');
  await sleep(3000);
  shell('shell','uiautomator','dump','/sdcard/atrium-smoke.xml');
  assert(shell('shell','cat','/sdcard/atrium-smoke.xml').includes('正在重新连接'),'native offline UI missing');
  const started=Date.now();shell('shell','svc','wifi','enable');restoreWifi=false;
  await attach();await until(onlineHome,'network recovery',60000);
  console.log(JSON.stringify({check:'offline cold start and automatic network recovery',elapsed_ms:Date.now()-started,result:'PASS'}));
 }
 console.log(JSON.stringify({result:'PASS',checks:['fullscreen viewport','home photo entry','four collections','previous/next','source card and scroll','operations Back ownership','held Enter','settings three panels and recovery','settings refresh preserves source','root native menu','show decode','refresh retains viewer','foreground recovery','process restart pairing']}));
} finally {if(restoreWifi)shell('shell','svc','wifi','enable');ws?.close();shell('forward','--remove','tcp:9223');}
