#!/usr/bin/env python3
"""Run only generated synthetic fixtures; write evidence and original-frame overlays.
Usage: evaluate_fixtures.py HELPER FIXTURE_RENDERER FIXTURES OUTPUT
The renderer is a separate test-only binary. Production helper never writes files.
"""
import json, os, subprocess, sys, time, hashlib, statistics
from pathlib import Path
from PIL import Image, ImageDraw
helper,renderer,fixtures,output=sys.argv[1:];fixtures=Path(fixtures);output=Path(output);output.mkdir(parents=True,exist_ok=True)
manifest=json.loads((fixtures/'manifest.json').read_text());summary={'generator_versions':manifest['generator_versions'],'files':[]}
def run(exe,path,args=()):
 with path.open('rb') as source:
  assert source.fileno()==3,'run without other inherited descriptors'
  start=time.monotonic();p=subprocess.run([exe,'--protocol=1',*args],input=b'{"protocol":1}\n',pass_fds=(3,),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=120,env={'PATH':'/usr/bin:/bin','LANG':'en_US.UTF-8','LC_ALL':'en_US.UTF-8'})
  elapsed=time.monotonic()-start
 if p.returncode:raise RuntimeError(f'{path.name}: helper exit {p.returncode}')
 return json.loads(p.stdout),elapsed
for source in sorted(fixtures.iterdir()):
 if source.suffix not in ('.png','.jpg','.webp','.tiff','.pdf') or source.name in ('geometry-base.pdf','conflict.png'):continue
 folder=output/source.name;folder.mkdir(exist_ok=True)
 wire,seconds=run(helper,source);(folder/'wire.json').write_text(json.dumps(wire,ensure_ascii=False,indent=2))
 run(renderer,source,[str(folder)])
 item={'file':source.name,'sha256':hashlib.sha256(source.read_bytes()).hexdigest(),'status':wire['status'],'seconds':round(seconds,3),'pages':len(wire['pages']),'observations':len(wire['segments'])}
 for page in wire['pages']:
  p=page['page'];raster=folder/f'page-{p}.png'
  if not raster.exists():continue
  im=Image.open(raster).convert('RGB');draw=ImageDraw.Draw(im)
  for obs in wire['segments']:
   if obs['page']!=p:continue
   x,y,w,h=obs['rect'];W,H=im.size;draw.rectangle((x*W,y*H,(x+w)*W,(y+h)*H),outline='red' if obs['origin']=='native' else 'blue',width=2)
  im.save(folder/f'page-{p}-overlay.png')
 if source.name.startswith(('report.','exif-','physical-')):
  fields=[]
  for expected in manifest['image_fields']:
   rect=expected['rect'];x,y,w,h=rect
   if source.name=='physical-90.png':rect=[1-y-h,x,h,w]
   if source.name=='physical-180.png':rect=[1-x-w,1-y-h,w,h]
   if source.name=='physical-270.png':rect=[y,1-x-w,h,w]
   best=0
   for obs in wire['segments']:
    if ''.join(obs['text'].split())!=''.join(expected['text'].split()):continue
    a,b=rect,obs['rect'];inter=max(0,min(a[0]+a[2],b[0]+b[2])-max(a[0],b[0]))*max(0,min(a[1]+a[3],b[1]+b[3])-max(a[1],b[1]));iou=inter/(a[2]*a[3]+b[2]*b[3]-inter);best=max(best,iou)
   fields.append({'text':expected['text'],'iou':round(best,3),'matched':best>=.4})
  item['field_locations']=fields;item['matched_fields']=sum(x['matched'] for x in fields)
 summary['files'].append(item);summary['engine']=wire['engine']
seconds=[f['seconds'] for f in summary['files']];summary['timing']={'median':statistics.median(seconds),'maximum':max(seconds),'total':round(sum(seconds),3)}
(output/'summary.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2));print(json.dumps({'files':len(summary['files']),'timing':summary['timing'],'field_checks':sum(len(x.get('field_locations',[])) for x in summary['files']),'matched':sum(x.get('matched_fields',0) for x in summary['files'])},indent=2))
