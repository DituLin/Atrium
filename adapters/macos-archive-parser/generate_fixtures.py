#!/usr/bin/env python3
"""Synthetic only. Outputs originals + expected display-space field rectangles."""
import json, sys, io
from pathlib import Path
from PIL import Image, ImageDraw, ImageFont
from reportlab.pdfgen import canvas
from reportlab.lib.utils import ImageReader
from pypdf import PdfReader, PdfWriter
from pypdf.generic import RectangleObject, NumberObject
out=Path(sys.argv[1]); out.mkdir(parents=True,exist_ok=True)
font=ImageFont.truetype('/System/Library/Fonts/Supplemental/Arial Unicode.ttf',44)
small=ImageFont.truetype('/System/Library/Fonts/Supplemental/Arial Unicode.ttf',36)
im=Image.new('RGB',(1800,1600),'white');d=ImageDraw.Draw(im)
rows=[(100,100,'合成健康报告 SYNTHETIC REPORT'),(100,230,'姓名：林明 Alice Lin'),(100,330,'出生日期：1988-03-12'),(100,430,'报告日期：2026-10-04'),(100,600,'项目'),(650,600,'数值'),(980,600,'单位'),(1280,600,'参考区间'),(100,710,'血红蛋白'),(650,710,'145.6'),(980,710,'g/L'),(1280,710,'130-175'),(100,850,'费用 Amount'),(650,850,'1234.50'),(980,850,'CNY'),(100,990,'变化 Delta'),(650,990,'-2.75'),(980,990,'mmol/L'),(100,1150,'医生：王华 Dr Wang')]
fields=[]
for x,y,t in rows:
 d.text((x,y),t,font=font,fill='black');box=d.textbbox((x,y),t,font=font);fields.append({'text':t,'rect':[box[0]/1800,box[1]/1600,(box[2]-box[0])/1800,(box[3]-box[1])/1600]})
im.save(out/'report.png');im.save(out/'report.jpg',quality=96);im.save(out/'report.webp',lossless=True)
im.transpose(Image.Transpose.ROTATE_270).save(out/'physical-90.png')
im.transpose(Image.Transpose.ROTATE_180).save(out/'physical-180.png')
im.transpose(Image.Transpose.ROTATE_90).save(out/'physical-270.png')
# Encode the inverse orientation so every EXIF-corrected display is the original.
inv={1:None,2:Image.Transpose.FLIP_LEFT_RIGHT,3:Image.Transpose.ROTATE_180,4:Image.Transpose.FLIP_TOP_BOTTOM,5:Image.Transpose.TRANSPOSE,6:Image.Transpose.ROTATE_90,7:Image.Transpose.TRANSVERSE,8:Image.Transpose.ROTATE_270}
for n,op in inv.items():
 raw=im if op is None else im.transpose(op);ex=Image.Exif();ex[274]=n;raw.save(out/f'exif-{n}.jpg',quality=96,exif=ex)
Image.new('RGB',(600,400),'white').save(out/'blank.png')
im.save(out/'multi.tiff',save_all=True,append_images=[im])
# Header property preflight; PNG stays highly compressed, no native full decode expected.
Image.new('1',(16385,2),1).save(out/'oversize.png')
(out/'bad.png').write_bytes(b'\x89PNG\r\n\x1a\ninvalid')
manifest={'image_fields':fields,'size':[1800,1600]}
# Native / scanned / scan-body+native footer / mixed.
for name in ('native','scanned','footer','mixed'):
 c=canvas.Canvas(str(out/f'{name}.pdf'),pagesize=(900,800),invariant=1)
 count=3 if name=='mixed' else 1
 for pg in range(count):
  if name=='native' or name=='mixed' and pg==0:
   for y,t in [(720,'Alice Lin DOB 1988-03-12'),(650,'Report 2026-10-04'),(550,'Amount 1234.50 CNY'),(450,'Delta -2.75 mmol/L'),(350,'Range 130-175')]:c.setFont('Helvetica',24);c.drawString(50,y,t)
  elif name!='mixed' or pg==1:
   c.drawImage(ImageReader(im),0,0,width=900,height=800)
  if name=='footer':c.setFont('Helvetica',10);c.drawString(40,10,'Synthetic footer 2026')
  c.showPage()
 c.save()
# Invisible/native conflicting number over an actual raster number.
conf=Image.new('RGB',(900,400),'white');cd=ImageDraw.Draw(conf);cf=ImageFont.truetype('/System/Library/Fonts/Helvetica.ttc',64);cd.text((100,120),'TOTAL 9.30',font=cf,fill='black');conf.save(out/'conflict.png')
c=canvas.Canvas(str(out/'conflict.pdf'),pagesize=(450,200),invariant=1);c.drawImage(ImageReader(conf),0,0,width=450,height=200);t=c.beginText(50,109);t.setFont('Helvetica',32);t.setTextRenderMode(3);t.textLine('TOTAL 9.80');c.drawText(t);c.save()
# Nonzero boxes, outside/partial crop, rotations, noninteger raster scale.
c=canvas.Canvas(str(out/'geometry-base.pdf'),pagesize=(410,320),invariant=1)
for rot in [0,90,180,270]:
 c.setFillColorRGB(1,0,0);c.rect(60,200,10,10,fill=1,stroke=0);c.setFillColorRGB(0,0,0);c.setFont('Helvetica',16);c.drawString(80,190,'ALPHA 12.34');c.drawString(80,95,'BETA 56.78');c.drawString(20,140,'CLIPPED 123.45');c.drawString(300,250,'OUTSIDE');c.showPage()
c.save();r=PdfReader(out/'geometry-base.pdf');w=PdfWriter()
for page,rot in zip(r.pages,[0,90,180,270]):page.mediabox=RectangleObject([10,20,410,320]);page.cropbox=RectangleObject([50,70,290.3,230.7]);page[__import__('pypdf').generic.NameObject('/Rotate')]=NumberObject(rot);w.add_page(page)
with open(out/'geometry.pdf','wb') as f:w.write(f)
# Crop extends beyond media: use actual intersection, not CropBox alone.
r=PdfReader(out/'geometry-base.pdf');p=r.pages[0];p.mediabox=RectangleObject([50,70,290.3,230.7]);p.cropbox=RectangleObject([10,20,410,320]);w=PdfWriter();w.add_page(p)
with open(out/'crop-intersection.pdf','wb') as f:w.write(f)
# Native PDF non-BMP glyph uses a ToUnicode mapping through an embedded font.
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
pdfmetrics.registerFont(TTFont('DejaVu','/System/Library/Fonts/Supplemental/Arial Unicode.ttf')) if Path('/System/Library/Fonts/Supplemental/Arial Unicode.ttf').exists() else None
# Deterministic minimal PDF: ToUnicode maps code 01 to U+1F600, not UTF-16 length 1.
objs=[b'<< /Type /Catalog /Pages 2 0 R >>',b'<< /Type /Pages /Count 1 /Kids [3 0 R] >>',b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding /ToUnicode 6 0 R >>']
stream=b'BT /F1 24 Tf 40 100 Td <01412031322E3334> Tj ET';objs.append(b'<< /Length '+str(len(stream)).encode()+b' >>\nstream\n'+stream+b'\nendstream')
cmap=b'/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def /CMapName /Synthetic def /CMapType 2 def 1 begincodespacerange <00> <FF> endcodespacerange 1 beginbfchar <01> <D83DDE00> endbfchar endcmap CMapName currentdict /CMap defineresource pop end end';objs.append(b'<< /Length '+str(len(cmap)).encode()+b' >>\nstream\n'+cmap+b'\nendstream')
b=bytearray(b'%PDF-1.4\n');offsets=[0]
for i,o in enumerate(objs,1):offsets.append(len(b));b+=f'{i} 0 obj\n'.encode()+o+b'\nendobj\n'
x=len(b);b+=f'xref\n0 {len(offsets)}\n0000000000 65535 f \n'.encode()
for offset in offsets[1:]:b+=f'{offset:010d} 00000 n \n'.encode()
b+=f'trailer << /Size {len(offsets)} /Root 1 0 R >>\nstartxref\n{x}\n%%EOF\n'.encode();(out/'nonbmp.pdf').write_bytes(b)
w=PdfWriter();w.append(out/'native.pdf');w.encrypt('synthetic-password')
with open(out/'locked.pdf','wb')as f:w.write(f)
w=PdfWriter();w.append(out/'native.pdf');w.encrypt('',owner_password='synthetic-owner',permissions_flag=0)
with open(out/'restricted.pdf','wb')as f:w.write(f)
w=PdfWriter()
for _ in range(101):w.add_blank_page(width=100,height=100)
with open(out/'101-pages.pdf','wb')as f:w.write(f)
# Mixed readable/unrenderable page: deterministic partial coverage fixture.
w=PdfWriter();w.append(out/'native.pdf');w.add_blank_page(width=2000000000,height=200)
with open(out/'partial.pdf','wb')as f:w.write(f)
w=PdfWriter();w.add_blank_page(width=300,height=200)
with open(out/'blank.pdf','wb')as f:w.write(f)
(out/'bad.pdf').write_bytes(b'%PDF-1.7\nnot a PDF')
from pypdf.generic import DictionaryObject, ArrayObject, TextStringObject, NameObject
w=PdfWriter();w.append(out/'native.pdf')
annotation=DictionaryObject({NameObject('/Type'):NameObject('/Annot'),NameObject('/Subtype'):NameObject('/Text'),NameObject('/Rect'):RectangleObject([40,20,100,50]),NameObject('/Contents'):TextStringObject('Synthetic excluded annotation')})
widget=DictionaryObject({NameObject('/Type'):NameObject('/Annot'),NameObject('/Subtype'):NameObject('/Widget'),NameObject('/FT'):NameObject('/Tx'),NameObject('/T'):TextStringObject('Synthetic form'),NameObject('/V'):TextStringObject('Synthetic excluded value'),NameObject('/Rect'):RectangleObject([100,20,250,60])})
a=w._add_object(annotation);v=w._add_object(widget);w.pages[0][NameObject('/Annots')]=ArrayObject([a,v]);w._root_object[NameObject('/AcroForm')]=w._add_object(DictionaryObject({NameObject('/Fields'):ArrayObject([v])}))
with open(out/'annotations.pdf','wb')as f:w.write(f)
manifest['generator_versions']={'pillow':__import__('PIL').__version__,'pypdf':__import__('pypdf').__version__,'reportlab':__import__('reportlab').Version}
(out/'manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2))
print(out)

# Integrity regressions: preserve synthetic originals and include valid framing
# variants so a completeness guard cannot simply reject a format or all images.
import struct, zlib
integrity=out/'integrity';integrity.mkdir(exist_ok=True)
def chunk(kind,payload):
 return struct.pack('>I',len(payload))+kind+payload+struct.pack('>I',zlib.crc32(kind+payload))
def png(w,h,raw,depth=8,color=2,interlace=0):
 return b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR',struct.pack('>IIBBBBB',w,h,depth,color,0,0,interlace))+chunk(b'IDAT',zlib.compress(raw))+chunk(b'IEND',b'')
for ext in ['jpg','png','webp']:
 source=(out/f'report.{ext}').read_bytes()
 for numerator in [1,2,3,95]:
  denominator=100 if numerator==95 else 4
  (integrity/f'truncated-{numerator}-{denominator}.{ext}').write_bytes(source[:len(source)*numerator//denominator])
 if ext=='jpg':
  (integrity/'truncated-appended-eoi.jpg').write_bytes(source[:len(source)//2]+b'\xff\xd9')
  # Embedded EOI bytes in a metadata segment must not count as the terminator.
  (integrity/'embedded-eoi-missing-end.jpg').write_bytes(source[:2]+b'\xff\xfe\x00\x04\xff\xd9'+source[2:-2])
  (integrity/'valid-comment-eoi.jpg').write_bytes(source[:2]+b'\xff\xfe\x00\x04\xff\xd9'+source[2:])
 if ext=='png':
  offset=8;parts=[];idat=b''
  while offset<len(source):
   n=int.from_bytes(source[offset:offset+4],'big');kind=source[offset+4:offset+8];payload=source[offset+8:offset+8+n];parts.append((kind,payload));offset+=n+12
   if kind==b'IDAT':idat+=payload
  ihdr=next(payload for kind,payload in parts if kind==b'IHDR')
  (integrity/'valid-split-idat.png').write_bytes(source[:8]+chunk(b'IHDR',ihdr)+chunk(b'IDAT',b'')+chunk(b'IDAT',idat[:1])+chunk(b'IDAT',idat[1:7])+chunk(b'IDAT',idat[7:])+chunk(b'IDAT',b'')+chunk(b'IEND',b''))
  (integrity/'truncated-reframed-idat.png').write_bytes(source[:8]+chunk(b'IHDR',ihdr)+chunk(b'IDAT',idat[:len(idat)//2])+chunk(b'IEND',b''))
  bad=bytearray(source);bad[41]^=1;(integrity/'bad-crc.png').write_bytes(bad)
  (integrity/'missing-iend.png').write_bytes(source[:-12])
  (integrity/'trailing-data.png').write_bytes(source+b'junk')
  (integrity/'bad-zlib-checksum.png').write_bytes(source[:8]+chunk(b'IHDR',ihdr)+chunk(b'IDAT',idat[:-1]+bytes([idat[-1]^1]))+chunk(b'IEND',b''))
 if ext=='webp':
  cut=bytearray(source[:len(source)//2]);cut[4:8]=struct.pack('<I',len(cut)-8);(integrity/'truncated-reframed-riff.webp').write_bytes(cut)
# Small valid PNG variants exercise exact row length/Adam7/sub-byte paths.
(integrity/'valid-gray1.png').write_bytes(png(17,9,b'\x00\xff\xff\x80'*9,1,0))
(integrity/'valid-gray16.png').write_bytes(png(17,9,(b'\x00'+b'\xff\xff'*17)*9,16,0))
w,h=17,9;raw=b''
for sx,sy,dx,dy in [(0,0,8,8),(4,0,8,8),(0,4,4,8),(2,0,4,4),(0,2,2,4),(1,0,2,2),(0,1,1,2)]:
 pw=max(0,(w-sx+dx-1)//dx);ph=max(0,(h-sy+dy-1)//dy)
 if pw and ph:raw+=(b'\x00'+b'\xff\xff\xff'*pw)*ph
(integrity/'valid-adam7.png').write_bytes(png(w,h,raw,8,2,1))
(integrity/'invalid-filter.png').write_bytes(png(17,9,(b'\x05'+b'\xff'*51)*9))
(integrity/'excess-inflate.png').write_bytes(png(17,9,(b'\x00'+b'\xff'*51)*9+b'\x00'))
(integrity/'short-inflate.png').write_bytes(png(17,9,(b'\x00'+b'\xff'*51)*8))
im.save(integrity/'valid-progressive.jpg',quality=96,progressive=True)
im.convert('L').save(integrity/'valid-grayscale.jpg',quality=96)
im.convert('CMYK').save(integrity/'valid-cmyk.jpg',quality=96)

# Resource-positive JPEG: within the declared 60 MP input limit. Strict entropy
# validation scales output to 1/8 and allows bounded progressive coefficient work.
large=Image.new('RGB',(6000,6000),'white');large.save(integrity/'valid-large-progressive.jpg',progressive=True,quality=90)
# RIFF chunks have explicit lengths and odd-length payload padding.
webp=(out/'report.webp').read_bytes()
for name,tail in [('valid-odd-chunk.webp',b'XTRA'+struct.pack('<I',1)+b'x\x00'),('missing-riff-padding.webp',b'XTRA'+struct.pack('<I',1)+b'x')]:
 content=bytearray(webp+tail);content[4:8]=struct.pack('<I',len(content)-8);(integrity/name).write_bytes(content)
im.save(integrity/'valid-lossy.webp',quality=90)
alpha=im.convert('RGBA');alpha.putalpha(192);alpha.save(integrity/'valid-alpha.webp',lossless=True)
# Repair both outer and inner length fields, so ImageIO must reject incomplete
# VP8/VP8L payloads even when the RIFF framing itself is internally consistent.
for kind,path in [('lossless',out/'report.webp'),('lossy',integrity/'valid-lossy.webp')]:
 source=path.read_bytes();assert source[12:16] in (b'VP8L',b'VP8 ')
 n=int.from_bytes(source[16:20],'little')//2
 cut=bytearray(source[:20+n]+(b'\x00' if n%2 else b''))
 cut[16:20]=struct.pack('<I',n);cut[4:8]=struct.pack('<I',len(cut)-8)
 (integrity/f'truncated-reframed-{kind}.webp').write_bytes(cut)

# ImageIO can recover plausible thumbnails from damaged unvalidated codecs.
# Preserve useful OCR, but none of these formats may claim complete coverage.
unvalidated=out/'unvalidated';unvalidated.mkdir(exist_ok=True)
im.save(unvalidated/'report.bmp')
im.save(unvalidated/'report.gif')
im.save(unvalidated/'report.tiff',compression='tiff_lzw')
(unvalidated/'bad.bmp').write_bytes(b'BMbroken')
im.save(unvalidated/'multi.gif',save_all=True,append_images=[im.transpose(Image.Transpose.FLIP_LEFT_RIGHT)])
Image.new('RGB',(600,400),'white').save(unvalidated/'blank.bmp')
bmp=(unvalidated/'report.bmp').read_bytes()
for n in [1,2]: (unvalidated/f'truncated-{n}-4.bmp').write_bytes(bmp[:len(bmp)*n//4])
# Existing macOS ImageIO encoder, not a Python/production codec dependency.
import subprocess
subprocess.run(['/usr/bin/sips','-s','format','heic',str(out/'report.png'),'--out',str(unvalidated/'report.heic')],check=True,stdout=subprocess.DEVNULL)
