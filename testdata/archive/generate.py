"""Deterministic, fictional parser fixtures; standard-library Python, no runtime dependency."""
from pathlib import Path
from zipfile import ZipFile, ZipInfo, ZIP_DEFLATED
ROOT=Path(__file__).parent
W='http://schemas.openxmlformats.org/wordprocessingml/2006/main'
S='http://schemas.openxmlformats.org/spreadsheetml/2006/main'
R='http://schemas.openxmlformats.org/officeDocument/2006/relationships'
P='http://schemas.openxmlformats.org/package/2006/relationships'
C='http://schemas.openxmlformats.org/package/2006/content-types'
DOC='application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml'
XLS='application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml'
def rels(items):
 return '<Relationships xmlns="'+P+'">'+''.join('<Relationship Id="%s" Type="%s/%s" Target="%s"%s/>'%(i,R,k,t,extra) for i,k,t,extra in items)+'</Relationships>'
def package(main,typ,parts):
 return {'[Content_Types].xml':'<Types xmlns="'+C+'"><Default Extension="xml" ContentType="application/xml"/><Override PartName="/'+main+'" ContentType="'+typ+'"/></Types>', '_rels/.rels':rels([('main','officeDocument',main,'')]),**parts}
def save(name,parts):
 with ZipFile(ROOT/name,'w') as z:
  for path,data in parts.items():
   info=ZipInfo(path,(2020,1,1,0,0,0));info.compress_type=ZIP_DEFLATED;info.external_attr=0o100644<<16
   z.writestr(info,data.encode() if isinstance(data,str) else data)
def para(t):return '<w:p><w:r><w:t xml:space="preserve">'+t+'</w:t></w:r></w:p>'
def cell(t,pr=''):return '<w:tc><w:tcPr>'+pr+'</w:tcPr>'+para(t)+'</w:tc>'
def doc(body):return package('content/main.xml',DOC,{'content/main.xml':'<w:document xmlns:w="'+W+'"><w:body>'+body+'</w:body></w:document>'})
e01=doc(para('学生李妍 出生2012-11-06 2026春季')+'<w:tbl><w:tblGrid><w:gridCol/><w:gridCol/></w:tblGrid><w:tr>'+cell('数学')+cell('88/100')+'</w:tr><w:tr>'+cell('语文')+cell('91/100')+'</w:tr></w:tbl>'+para('示例学校'))
save('e01.docx',e01)
mergebody=para('前文')+'<w:tbl><w:tr>'+cell('合并标题','<w:gridSpan w:val="2"/><w:vMerge w:val="restart"/>')+cell('右侧')+'</w:tr><w:tr>'+cell('','<w:gridSpan w:val="2"/><w:vMerge/>')+'<w:tc>'+para('父单元格')+'<w:tbl><w:tr>'+cell('嵌套')+'</w:tr></w:tbl>'+para('后段')+'</w:tc></w:tr></w:tbl><w:p><w:r><w:t>A</w:t><w:tab/><w:t>B</w:t><w:br/><w:t>C</w:t></w:r></w:p>'
save('merges.docx',doc(mergebody))
partial=doc(para('当前正文')+'<w:del>'+para('已删除文本')+'</w:del><w:p><w:r><w:drawing/></w:r></w:p><w:p><w:r><w:txbxContent>'+para('文本框')+'</w:txbxContent></w:r></w:p>')
partial['content/header.xml']='<w:hdr xmlns:w="'+W+'">'+para('页眉')+'</w:hdr>'
partial['content/_rels/main.xml.rels']=rels([('h','header','header.xml',''),('link','hyperlink','https://example.invalid/',' TargetMode="External"')])
save('partial.docx',partial)
def book(sheet,name='费用',date1904=False):
 main='<workbook xmlns="'+S+'" xmlns:r="'+R+'"><workbookPr date1904="'+('1' if date1904 else '0')+'"/><sheets><sheet name="'+name+'" sheetId="7" r:id="sheetRel" state="hidden"/></sheets></workbook>'
 parts={'xl/book.xml':main,'xl/_rels/book.xml.rels':rels([('sheetRel','worksheet','../sheets/data.xml',''),('ss','sharedStrings','strings.xml',''),('sty','styles','styles.xml','')]),'sheets/data.xml':'<worksheet xmlns="'+S+'">'+sheet+'</worksheet>','xl/strings.xml':'<sst xmlns="'+S+'"><si><r><t>学生</t></r><r><t>李妍</t></r></si><si><t>示例物业</t></si></sst>','xl/styles.xml':'<styleSheet xmlns="'+S+'"><numFmts count="2"><numFmt numFmtId="164" formatCode="unsupported-format"/><numFmt numFmtId="165" formatCode="yyyy-mm-dd"/></numFmts><cellXfs count="5"><xf numFmtId="0"/><xf numFmtId="2"/><xf numFmtId="14"/><xf numFmtId="164"/><xf numFmtId="165"/></cellXfs></styleSheet>'}
 return package('xl/book.xml',XLS,parts)
def xc(ref,v=None,t=None,s=None,formula=None,inline=None):
 return '<c r="'+ref+'"'+(' t="'+t+'"' if t else '')+(' s="'+str(s)+'"' if s is not None else '')+'>'+('' if formula is None else formula)+('' if v is None else '<v>'+v+'</v>')+('' if inline is None else '<is><t>'+inline+'</t></is>')+'</c>'
e02=book('<dimension ref="A1:XFD1048576"/><sheetData><row r="1">'+xc('A1','0','s')+'</row><row r="2">'+xc('A2',t='inlineStr',inline='出生2012-11-06 2026秋季')+'</row><row r="3">'+xc('A3',t='inlineStr',inline='数学')+xc('C3','132')+xc('D3','150')+'</row></sheetData><mergeCells><mergeCell ref="A1:B1"/></mergeCells>','成绩')
save('e02.xlsx',e02)
frows='<cols><col min="2" max="2" hidden="1"/></cols><sheetData><row r="1">'+xc('A1',t='inlineStr',inline='账单')+xc('B1',t='inlineStr',inline='金额')+'</row>'
for i,ident,amount in [(2,'TEST-BILL-001','128.10'),(3,'TEST-BILL-002','71.90')]:
 frows+='<row r="%s" hidden="1">'%i+xc('A'+str(i),t='inlineStr',inline=ident)+xc('B'+str(i),amount,s=1)+xc('C'+str(i),'1','s')+xc('D'+str(i),t='inlineStr',inline='CNY')+xc('E'+str(i),t='inlineStr',inline='2026-10-01')+'</row>'
frows+='<row r="4">'+xc('B4','200.00',s=1,formula='<f>SUM(B2:B3)</f>')+'</row><row r="10">'+xc('A10')+xc('B10',t='inlineStr',inline='')+xc('C10','0')+xc('E10',formula='<f>SUM(B2:B3)</f>')+xc('F10','',t='str',formula='<f>""</f>')+'</row><row r="11">'+xc('F11','2',formula='<f t="shared" si="7" ref="F11:F12">C10+2</f>')+'</row><row r="12">'+xc('F12',formula='<f t="shared" si="7"/>')+'</row><row r="20">'+xc('A20','1','b')+xc('B20','#DIV/0!','e')+xc('C20','2026-10-03','d')+xc('D20','25',s=3)+'</row></sheetData>'
f01=book(frows)
save('f01.xlsx',f01)
missing=dict(f01);missing['sheets/data.xml']=missing['sheets/data.xml'].replace('<v>200.00</v>','');save('f01-missing-cache.xlsx',missing)
for d in [False,True]:save('dates-'+('1904' if d else '1900')+'.xlsx',book('<sheetData><row r="1">'+xc('A1','0',s=4)+xc('B1','59',s=4)+xc('C1','60',s=4)+xc('D1','61',s=4)+'</row></sheetData>',date1904=d))
(ROOT/'feeding.txt').write_bytes('张明 1980-03-12 医生周青\r\n喂养70ml\r\n喂养70ml\r\n'.encode())
(ROOT/'home.txt').write_text('地址：测试苑1号测试室\n型号：DEMO-AC\n保修到期：2028-05-31\n')
(ROOT/'school.txt').write_text('示例学校运动会2026年10月\n')
(ROOT/'f02.csv').write_text('账单,金额,币种,日期,收款方\nTEST-BILL-001,128.10,CNY,2026-10-01,示例物业\nTEST-BILL-003,20.00,USD,2026-10-02,示例物业\n')
for name,changes in [('wrong-content-type',{'[Content_Types].xml':'<Types xmlns="'+C+'"><Override PartName="/content/main.xml" ContentType="application/xml"/></Types>'}),('escaping-relationship',{'_rels/.rels':rels([('main','officeDocument','../escape.xml','')])}),('path-traversal',{'../escape.xml':'bad'}),('dtd',{'content/main.xml':'<!DOCTYPE document [<!ENTITY x "evil">]><w:document xmlns:w="'+W+'"><w:body>'+para('&x;')+'</w:body></w:document>'})]:
 save('badpackages/'+name+'.zip',{**e01,**changes})
(ROOT/'badpackages'/'compound.bin').write_bytes(bytes.fromhex('d0cf11e0a1b11ae1')+b'not proof of encryption')
(ROOT/'badpackages'/'corrupt.zip').write_bytes(b'PK truncated')
print('fixtures generated')
