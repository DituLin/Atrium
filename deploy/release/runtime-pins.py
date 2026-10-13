"""Record exact host runtime bindings, without reading credentials."""
import hashlib, json, os, pathlib, subprocess, sys
def tree_hash(root):
 h=hashlib.sha256(); count=0; total=0
 for p in sorted(root.rglob('*')):
  rel=p.relative_to(root).as_posix()
  if p.is_symlink():
   target=p.resolve(strict=True)
   if not target.is_relative_to(root) and not (rel=='node_modules/openclaw' and target==pathlib.Path(out['openclaw']['path'])):
    raise SystemExit('runtime symlink escapes trusted binding')
   h.update(('link:'+rel+':'+os.readlink(p)+'\n').encode());continue
  if p.is_file():
   count+=1;total+=p.stat().st_size
   if count>100000 or total>2*1024**3: raise SystemExit('runtime fingerprint bounds exceeded')
   digest=hashlib.file_digest(p.open('rb'),'sha256').hexdigest() if hasattr(hashlib,'file_digest') else hashlib.sha256(p.read_bytes()).hexdigest()
   h.update((rel+':'+digest+'\n').encode())
 return {'tree_sha256':h.hexdigest(),'files':count,'bytes':total}
out={}
for key,expected in [('NODE','v24.21.0'),('OPENCLAW','2026.9.8'),('PROVIDER','2026.9.8')]:
 raw=pathlib.Path(os.environ['ATRIUM_RELEASE_'+key])
 if not raw.is_absolute() or '.codex' in raw.parts: raise SystemExit('untrusted runtime path')
 p=raw.resolve(strict=True)
 if '.codex' in p.parts: raise SystemExit('Codex runtime is not a release dependency')
 if key=='NODE':
  version=subprocess.check_output([str(p),'--version'],text=True).strip()
  digest=hashlib.sha256(p.read_bytes()).hexdigest()
 else:
  version=json.loads((p/'package.json').read_text())['version']
  digest=hashlib.sha256((p/'package.json').read_bytes()).hexdigest()
 if version!=expected: raise SystemExit('runtime version mismatch: '+key)
 out[key.lower()]={'path':str(p),'version':version,'sha256':digest}
 if key!='NODE': out[key.lower()].update(tree_hash(p))
provider=pathlib.Path(out['provider']['path'])
if (provider/'node_modules/openclaw').resolve(strict=True)!=pathlib.Path(out['openclaw']['path']): raise SystemExit('provider OpenClaw binding mismatch')
pathlib.Path(sys.argv[1]).write_text(json.dumps(out,indent=2)+'\n')
