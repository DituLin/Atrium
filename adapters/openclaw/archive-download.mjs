import {createDecipheriv,createHash} from 'node:crypto';
import {MAX_ARCHIVE_BYTES} from './archive-ingress.mjs';
const error=code=>Object.assign(new Error(`archive_${code}`),{code:`archive_${code}`});
const hosts={weixin:['novac2c.cdn.weixin.qq.com'],discord:['cdn.discordapp.com','media.discordapp.net']};
function checkedURL(provider){let url;try{url=new URL(provider.kind==='weixin'?(provider.media.full_url||`https://novac2c.cdn.weixin.qq.com/c2c/download?encrypted_query_param=${encodeURIComponent(provider.media.encrypt_query_param??'')}`):provider.url);}catch{throw error('url');}if(url.protocol!=='https:'||url.username||url.password||url.port||url.hash||!hosts[provider.kind]?.includes(url.hostname))throw error('url');return url;}
function keyFor(p){if(p.kind!=='weixin')return null;if(p.aeskey){if(!/^[a-fA-F0-9]{32}$/.test(p.aeskey))throw error('decrypt');return Buffer.from(p.aeskey,'hex');}const encoded=p.media?.aes_key;if(!encoded)return null;if(typeof encoded!=='string'||encoded.length>48||!/^[-A-Za-z0-9+/]*={0,2}$/.test(encoded))throw error('decrypt');const b=Buffer.from(encoded,'base64');if(b.length===16)return b;if(b.length===32&&/^[a-fA-F0-9]{32}$/.test(b.toString('ascii')))return Buffer.from(b.toString('ascii'),'hex');throw error('decrypt');}
// Bounded ciphertext and plaintext; caller only publishes after iterator finishes.
export async function* downloadArchive(provider,{signal,fetchImpl=fetch,maxBytes=MAX_ARCHIVE_BYTES,timeoutMs=120000}={}){
 const url=checkedURL(provider),key=keyFor(provider);const abort=AbortSignal.any([AbortSignal.timeout(timeoutMs),...(signal?[signal]:[])]);let response;
 try{response=await fetchImpl(url,{redirect:'error',signal:abort,credentials:'omit',referrerPolicy:'no-referrer'});}catch{throw error('download');}
 if(!response.ok||!response.body)throw error('download');const expected=Number(provider.size);if(Number.isFinite(expected)&&expected>maxBytes)throw error('too_large');
 const declared=Number(response.headers.get('content-length'));if(declared>maxBytes+(key?16:0)){await response.body.cancel();throw error('too_large');}
 let cipherBytes=0,plainBytes=0;const md5=createHash('md5');const decipher=key?createDecipheriv('aes-128-ecb',key,null):null;
 const bounded=b=>{plainBytes+=b.length;if(plainBytes>maxBytes)throw error('too_large');md5.update(b);return b;};
 try{for await(const b of response.body){if(abort.aborted)throw error('download');cipherBytes+=b.length;if(cipherBytes>maxBytes+(key?16:0))throw error('too_large');yield bounded(decipher?decipher.update(b):b);}if(decipher)yield bounded(decipher.final());}
 catch(e){if(e.code?.startsWith('archive_'))throw e;throw error(key?'decrypt':'download');}
 if(plainBytes===0)throw error('empty');if(Number.isSafeInteger(expected)&&expected>0&&plainBytes!==expected)throw error('integrity');
 const digest=md5.digest('hex');if(provider.md5&&(!/^[a-f0-9]{32}$/i.test(provider.md5)||provider.md5.toLowerCase()!==digest))throw error('integrity');
}
