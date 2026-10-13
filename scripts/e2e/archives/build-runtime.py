#!/usr/bin/env python3
"""Build one immutable acceptance runtime from a clean tracked revision."""
import hashlib,json,os,subprocess,sys,time
from pathlib import Path
root=Path(__file__).resolve().parents[3];out=Path(sys.argv[1]).resolve()
if out.exists():raise SystemExit('new runtime directory required')
if subprocess.check_output(['git','status','--porcelain','--untracked-files=no'],cwd=root):raise SystemExit('commit tracked runtime changes before freezing')
sha=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
out.mkdir(mode=0o700)
def digest(p):return hashlib.sha256(p.read_bytes()).hexdigest()
manifest={'code_sha':sha,'built_at':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'binaries':{},'resources':{}}
for name,pkg in [('core','./scripts/e2e/archives'),('host','./cmd/atrium-brain-host'),('mcp','./cmd/home-mcp')]:
 p=out/name;subprocess.run(['go','build','-o',str(p),pkg],cwd=root,check=True);p.chmod(0o500)
 manifest['binaries'][name]={'path':str(p),'sha256':digest(p)}
for base in ['adapters/openclaw','internal/archive/understanding']:
 for p in (root/base).rglob('*'):
  if p.is_file() and 'node_modules' not in p.parts:manifest['resources'][str(p.relative_to(root))]=digest(p)
p=out/'runtime-manifest.json';p.write_text(json.dumps(manifest,indent=2)+'\n');p.chmod(0o400)
print(p)
