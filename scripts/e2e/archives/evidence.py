#!/usr/bin/env python3
"""Collect reproducible synthetic-only evidence. No exit-code-only PASS grading."""
import hashlib,json,os,subprocess,sys,time
from pathlib import Path
root=Path(__file__).resolve().parents[3];d=Path(sys.argv[1]);fixtures=Path(sys.argv[2])
if (d/'SYNTHETIC-ONLY').read_text()!='Atrium synthetic archive acceptance v1\n':raise SystemExit('synthetic marker required')
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def command(args):return subprocess.check_output(args,cwd=root,text=True).strip()
files={p.relative_to(root).as_posix():sha(p) for base in ['scripts/e2e/archives','adapters/openclaw/archive-worker-plugin','internal/archive/understanding'] for p in (root/base).glob('*') if p.is_file()}
manifest=json.loads((fixtures/'manifest.json').read_text());assert manifest['synthetic_only']
for f in manifest['fixtures']:assert sha(fixtures/f['file'])==f['sha256']
turns=[]
def walk(x):
 if isinstance(x,dict):
  yield x
  for v in x.values():yield from walk(v)
 elif isinstance(x,list):
  for v in x:yield from walk(v)
for p in sorted(d.glob('query-*/captures/*.json')):
 if p.name.endswith('.meta.json'):continue
 try:x=json.loads(p.read_text())
 except (ValueError,UnicodeDecodeError):continue
 receipts=x.get('receipts',[]);documents=sorted({z['document_id'] for z in walk(receipts) if isinstance(z.get('document_id'),str)})
 evidence=[z for z in walk(receipts) if z.get('document_id') and z.get('locator') and z.get('segment_id')]
 turns.append({'case':p.name.split('-')[0],'capture':str(p.relative_to(d)),'sha256':sha(p),'failed':x.get('failed'),'timed_out':x.get('timed_out'),'tools':[r.get('tool') for r in receipts],'documents':documents,'evidence':evidence,'availability':[(r.get('tool_result') or {}).get('structuredContent',{}).get('availability') for r in receipts]})
model=[]
for p in sorted((d/'wire').glob('*-output.json')):
 try:x=json.loads(p.read_text())
 except (ValueError,UnicodeDecodeError):continue
 model.append({'capture':str(p.relative_to(d)),'sha256':sha(p),**{k:x[k] for k in ['usage','configuration_fingerprint','protocol_version','invocation_id','input_digest','provider','requested_model','model','response_model','stop_reason','execution','error'] if k in x}})
status={q['id']:{'status':'not_run','requirement':q.get('question',q.get('operation'))} for q in json.loads(Path(__file__).with_name('questions.json').read_text())}
for t in turns:status.setdefault(t['case'],{'requirement':'additional exploratory natural-language turn'}).update(status='needs_evidence_review',capture=t['capture'])
for p in sorted(d.glob('operations-*.json')):
 for k,v in json.loads(p.read_text()).items():status[k].update(status=v['status'],capture=p.name,reason=v.get('reason',''))
status['42'].update(status='deferred',reason='Requires T16 real isolated NAS restore; no PASS claimed')
out={'synthetic_only':True,'collected_at':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'code_sha':command(['git','rev-parse','HEAD']),'working_changes':command(['git','status','--porcelain']),'hardware':{'model':command(['sysctl','-n','hw.model']),'cpu_count':command(['sysctl','-n','hw.ncpu']),'memory_bytes':command(['sysctl','-n','hw.memsize']),'os':command(['sw_vers','-productVersion'])},'resources':files,'fixture_manifest_sha256':sha(fixtures/'manifest.json'),'fixtures':manifest,'model_attempts':model,'query_turns':turns,'cases':status}
p=d/'evidence-index.json';p.write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n');p.chmod(0o600)
print(json.dumps({'model_attempts':len(model),'query_turns':len(turns),'cases':{k:v['status'] for k,v in status.items()}},ensure_ascii=False))
