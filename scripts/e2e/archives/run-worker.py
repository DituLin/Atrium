#!/usr/bin/env python3
"""Load exactly one provider credential in memory; never source shell files.
Synthetic-only wrapper retains bounded original worker wire input/output for
failed attempts as well as successful products. No environment is persisted.
"""
import json, os, subprocess, sys, hashlib
from pathlib import Path

def provider_key():
    for p in [Path.home()/'.openclaw/.env',Path.home()/'.openclaw/service-env/ai.openclaw.gateway.env']:
        if not p.exists(): continue
        for line in p.read_text().splitlines():
            k,sep,v=line.partition('=')
            if sep and k.strip()=='DEEPSEEK_API_KEY':
                v=v.strip()
                if len(v)>1 and v[0]==v[-1] and v[0] in "\"'":v=v[1:-1]
                if v:return v
    raise SystemExit('DeepSeek credential unavailable')

if __name__=='__main__':
    runner,directory=sys.argv[1:3];d=Path(directory)
    if (d/'SYNTHETIC-ONLY').read_text()!='Atrium synthetic archive acceptance v1\n':raise SystemExit('synthetic marker required')
    runtime=json.loads((d/'runtime-manifest.json').read_text())
    if hashlib.sha256(Path(runner).read_bytes()).hexdigest()!=runtime['binaries']['core']['sha256']:raise SystemExit('frozen Core binary changed')
    root=Path(__file__).resolve().parents[3]
    for resource,expected in runtime['resources'].items():
        if hashlib.sha256((root/resource).read_bytes()).hexdigest()!=expected:raise SystemExit('frozen model/plugin resource changed')
    captures=d/'wire';captures.mkdir(mode=0o700,exist_ok=True)
    wrapper=d/'capture-node.py'
    wrapper.write_text('''#!/usr/bin/env python3
import os,subprocess,sys,uuid,json
from pathlib import Path
root=Path('''+repr(str(captures))+''')
id=uuid.uuid4().hex
request=sys.stdin.buffer.read(262145)
if len(request)>262144:sys.exit(2)
p=subprocess.run(['/opt/homebrew/bin/node',*sys.argv[1:]],input=request,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
for suffix,data in [('input.json',request),('output.json',p.stdout[:262144]),('stderr.txt',p.stderr[:4096])]:
 f=os.open(str(root/(id+'-'+suffix)),os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
 with os.fdopen(f,'wb') as h:h.write(data)
sys.stdout.buffer.write(p.stdout);sys.stderr.buffer.write(p.stderr);sys.exit(p.returncode)
''');wrapper.chmod(0o700)
    env=os.environ.copy();env['DEEPSEEK_API_KEY']=provider_key();env['ATRIUM_E2E_NODE']=str(wrapper)
    env['ATRIUM_NATIVE_HELPER']='/tmp/atriumos-t14-preview/helper'
    env['ATRIUM_DEEPSEEK_PLUGIN']=str(Path.home()/'.openclaw.pre-migration/npm/projects/openclaw-deepseek-provider-2481ed984b/node_modules/@openclaw/deepseek-provider')
    raise SystemExit(subprocess.call([runner,'work',directory],env=env))
