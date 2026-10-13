package archivespool

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenedRootAndEveryComponentNoFollow(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.Mkdir(filepath.Join(root, "media"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "media", "report")
	_ = os.WriteFile(path, []byte("original"), 0600)
	f, err := Open(root, "media/report")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Rename(filepath.Join(root, "media"), filepath.Join(root, "old"))
	_ = os.Symlink(t.TempDir(), filepath.Join(root, "media"))
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "original" {
		t.Fatal(string(b))
	}
	if f, err := Open(root, "media/report"); err == nil {
		f.Close()
		t.Fatal("followed changed directory")
	}
	for _, p := range []string{"../elsewhere", "/absolute", "old/../old/report"} {
		if f, e := Open(root, p); e == nil {
			f.Close()
			t.Fatal("accepted", p)
		}
	}
}
