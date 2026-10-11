#!/usr/bin/env python3
"""Consistent synthetic baseline backup, retaining immutable originals/products."""
import hashlib,json,shutil,sqlite3,sys,time
from pathlib import Path
source,dest=map(lambda s:Path(s).resolve(),sys.argv[1:3])
marker='Atrium synthetic archive acceptance v1\n'
if (source/'SYNTHETIC-ONLY').read_text()!=marker or dest.exists():raise SystemExit('synthetic source and new destination required')
dest.mkdir(mode=0o700)
with sqlite3.connect(f'file:{source}/core.sqlite?mode=ro',uri=True) as db:
 db.execute('BEGIN')
 pending=db.execute("select count(*) from archive_tasks where status in ('running','queued','retry_wait')").fetchone()[0]
 if pending and "--allow-pending" not in sys.argv[3:]:raise SystemExit('quiescent completed/waiting baseline required')
 with sqlite3.connect(dest/'core.sqlite') as target:db.backup(target)
 for name in ['originals']:
  shutil.copytree(source/name,dest/name)
 for name in ['SYNTHETIC-ONLY','state.json','runtime-manifest.json','tasks.json','search-authorized.json','search-public.json']:
  if (source/name).exists():shutil.copy2(source/name,dest/name)
 for p in dest.rglob('*'):
  if p.is_file():p.chmod(0o600)
  elif p.is_dir():p.chmod(0o700)
 hashes={str(p.relative_to(dest)):hashlib.sha256(p.read_bytes()).hexdigest() for p in dest.rglob('*') if p.is_file()}
 manifest={'synthetic_only':True,'source':str(source),'pending_tasks':pending,'complete_baseline':pending==0,'snapshot_at_unix':time.time(),'method':'SQLite online backup from pinned read transaction; immutable original files copied','hashes':hashes}
 p=dest/'snapshot-manifest.json';p.write_text(json.dumps(manifest,indent=2)+'\n');p.chmod(0o600)
print(dest)
