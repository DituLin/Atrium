import {openSync,closeSync,fstatSync,writeSync,fsyncSync,constants}from'node:fs';
import {isAbsolute}from'node:path';
export function archiveInputEnvelope(result){return {schema_version:'1',observed_at:new Date().toISOString(),availability:'available',data:result};}
export function writeArchiveReceipt({path,invocation,run,args,result}){
 if(!path&&!invocation)return;
 if(!isAbsolute(path??'')||typeof invocation!=='string'||!invocation||invocation.length>128||!/^[a-f0-9]{64}$/.test(run))throw Error('archive_receipt');
 const env=archiveInputEnvelope(result);const record={schema_version:1,invocation_id:invocation,run_id:run,tool:'home_accept_archive_input',arguments:args,tool_result:{structuredContent:env}};
 const line=Buffer.from(JSON.stringify(record)+'\n');if(line.length>65536)throw Error('archive_receipt');let fd;
 try{fd=openSync(path,constants.O_WRONLY|constants.O_APPEND|constants.O_NOFOLLOW);const st=fstatSync(fd);if(!st.isFile()||(st.mode&0o777)!==0o600||st.size+line.length>512*1024)throw Error('archive_receipt');if(writeSync(fd,line)!==line.length)throw Error('archive_receipt');fsyncSync(fd);}catch{throw Error('archive_receipt');}finally{if(fd!==undefined)closeSync(fd);}
}
