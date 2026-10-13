"""Generate after signing, or verify every packaged file with --verify."""
import hashlib,json,pathlib,sys
root=pathlib.Path(sys.argv[1]).resolve(strict=True)
marker=root/'.atrium-release-owned'
if not marker.is_file() or marker.is_symlink(): raise SystemExit('not an owned release')
files={}
for p in sorted(root.rglob('*')):
 if p.is_symlink(): raise SystemExit('release contains symlink')
 if p.is_file() and p != root/'manifest.json':
  files[p.relative_to(root).as_posix()]={'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'bytes':p.stat().st_size}
body={'format':'atrium-release-v1','commit':marker.read_text().strip(),'files':files}
p=root/'manifest.json'
if '--verify' in sys.argv:
 if json.loads(p.read_text())!=body: raise SystemExit('release manifest mismatch')
 print(json.dumps({'verified':True,'files':len(files),'commit':body['commit']}))
else:
 p.write_text(json.dumps(body,indent=2,sort_keys=True)+'\n')
