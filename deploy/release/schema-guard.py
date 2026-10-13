"""Read-only preflight for older binaries; never migrates or rolls back data.
Usage: schema-guard.py DB TARGET_MIGRATIONS_DIR TARGET_SCHEMA
Use an independently verified target release's migration files, not latest ones.
"""
import json,pathlib,sqlite3,sys
path=pathlib.Path(sys.argv[1]).resolve(strict=True)
root=pathlib.Path(sys.argv[2]).resolve(strict=True)
maximum=int(sys.argv[3])
known=[]
for p in sorted(root.glob('[0-9][0-9][0-9][0-9]_*.sql')):
 v=int(p.name[:4])
 if v<=maximum: known.append((v,p.name))
if not known or known[-1][0]!=maximum: raise SystemExit('target schema manifest incomplete')
db=sqlite3.connect(path.as_uri()+'?mode=ro',uri=True)
actual=db.execute('SELECT version,name FROM schema_migrations ORDER BY version').fetchall()
if actual!=known[:len(actual)] or len(actual)>len(known): raise SystemExit('incompatible schema: old binary must not open this database; preserve current data and restore separately')
print(json.dumps({'compatible':True,'schema':actual[-1][0] if actual else 0,'target_maximum':maximum}))
