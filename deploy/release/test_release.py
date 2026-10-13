import json,pathlib,sqlite3,subprocess,sys,tempfile,unittest
HERE=pathlib.Path(__file__).resolve().parent
class GuardTest(unittest.TestCase):
 def test_old_binary_refuses_new_database(self):
  with tempfile.TemporaryDirectory() as d:
   root=pathlib.Path(d);db=sqlite3.connect(root/'db');db.execute('create table schema_migrations(version integer,name text)');m=root/'migrations';m.mkdir();(m/'0001_first.sql').touch();db.execute("insert into schema_migrations values(1,'0001_first.sql')");db.commit()
   def check():return subprocess.run([sys.executable,str(HERE/'schema-guard.py'),str(root/'db'),str(m),'1'],capture_output=True)
   self.assertEqual(check().returncode,0);db.execute("insert into schema_migrations values(2,'0002_next.sql')");db.commit();self.assertNotEqual(check().returncode,0);self.assertEqual(db.execute('select count(*) from schema_migrations').fetchone()[0],2)
 def test_manifest_detects_mutation_and_extra_files(self):
  with tempfile.TemporaryDirectory() as d:
   root=pathlib.Path(d);(root/'.atrium-release-owned').write_text('abc');(root/'a').write_text('original')
   cmd=[sys.executable,str(HERE/'manifest.py'),d];subprocess.run(cmd,check=True,capture_output=True);self.assertEqual(subprocess.run(cmd+['--verify'],capture_output=True).returncode,0);(root/'a').write_text('changed');self.assertNotEqual(subprocess.run(cmd+['--verify'],capture_output=True).returncode,0)
if __name__=='__main__':unittest.main()
