// Real-device test. Requires a paired DEBUG APK in the foreground, Node 24+,
// ADB and an admin CLI on the host. Never prints credentials or photo content.
import {execFileSync} from 'node:child_process';
import assert from 'node:assert/strict';
const adb=process.env.ADB || 'adb';
const binary=process.env.ATRIUM_BINARY || 'atrium';
const config=process.env.ATRIUM_CONFIG;
const screen=process.env.ATRIUM_SCREEN;
assert(config && screen,'Set ATRIUM_CONFIG and ATRIUM_SCREEN');
function shell(...args){return execFileSync(adb,args,{encoding:'utf8',timeout:20000});}
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
function call(method,params={}){return new Promise((resolve,reject)=>{const i=++id;const timer=setTimeout(()=>{pending.delete(i);reject(new Error(`CDP timeout: ${method}`));},10000);pending.set(i,m=>{clearTimeout(timer);resolve(m)});ws.send(JSON.stringify({id:i,method,params}));});}
async function evaluate(expression){const r=await call('Runtime.evaluate',{expression,returnByValue:true});assert(!r.result?.exceptionDetails,'JS evaluation failed');return r.result.result.value;}
async function until(expression,label,ms=20000){const start=Date.now();while(Date.now()-start<ms){try{if(await evaluate(expression))return;}catch{}await sleep(200);}throw new Error(`Timeout: ${label}`);}
function command(kind,...args){const start=Date.now();const out=execFileSync(binary,['admin','screen',kind,screen,...args,'--wait','--config',config],{encoding:'utf8',timeout:25000});assert(/status\s+applied/.test(out),out);console.log(JSON.stringify({command:kind,elapsed_ms:Date.now()-start,status:'applied'}));}
async function key(n){shell('shell','input','keyevent',String(n));await sleep(250);}
try {
 shell('shell','am','start','-W','-n','io.atrium.tv/.MainActivity');
 await attach();
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
 command('navigate','--route','photos','--collection','all');
 await until("document.querySelectorAll('.thumb').length>1",'photo grid');
 const first=await evaluate("document.activeElement.getAttribute('aria-label')");
 await key(22);
 assert.notEqual(await evaluate("document.activeElement.getAttribute('aria-label')"),first,'D-pad right did not move focus');
 await key(23);
 await until("!!document.querySelector('.viewer__image')?.naturalWidth",'D-pad Enter photo');
 await key(4);
 await until("!!document.querySelector('.photogrid')",'Android Back to grid');
 const photo=await evaluate("document.querySelector('.thumb img').src.match(/photos\\/([^?]+)/)[1]");
 command('show','--photo',photo);
 await until("!!document.querySelector('.viewer__image')?.naturalWidth",'show image decoded');
 command('refresh');
 assert(await evaluate("!!document.querySelector('.viewer')"),'refresh changed route');
 command('navigate','--route','dashboard');
 await until("!!document.querySelector('.dashboard')",'dashboard restored');
 await key(3);shell('shell','am','start','-n','io.atrium.tv/.MainActivity');
 await until("document.body.innerText.includes('Online') && !!document.querySelector('.dashboard')",'foreground recovery');
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
  await attach();await until("!!document.querySelector('.dashboard') && [...document.querySelectorAll('.chip')].some(x=>x.innerText.includes('Link')&&x.innerText.includes('Online'))",'network recovery',60000);
  console.log(JSON.stringify({check:'offline cold start and automatic network recovery',elapsed_ms:Date.now()-started,result:'PASS'}));
 }
 console.log(JSON.stringify({result:'PASS',checks:['fullscreen viewport','D-pad right/enter','Android Back','show decode','refresh retains viewer','foreground recovery','process restart pairing']}));
} finally {if(restoreWifi)shell('shell','svc','wifi','enable');ws?.close();shell('forward','--remove','tcp:9223');}
