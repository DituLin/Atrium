package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderEnvProtected(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "previous")
	if e := loadProviderEnv(""); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("DEEPSEEK_API_KEY") != "previous" {
		t.Fatal("changed")
	}
	for _, c := range []struct {
		body string
		mode os.FileMode
		ok   bool
	}{{"DEEPSEEK_API_KEY=test-key\n", 0600, true}, {"DEEPSEEK_API_KEY=secret\n", 0644, false}, {"DEEPSEEK_API_KEY=secret\nDEEPSEEK_API_KEY=second\n", 0600, false}, {"DEEPSEEK_API_KEY=$(touch /tmp/no)\n", 0600, false}, {"OTHER=secret\n", 0600, false}} {
		p := filepath.Join(t.TempDir(), "env")
		os.WriteFile(p, []byte(c.body), c.mode)
		e := loadProviderEnv(p)
		if (e == nil) != c.ok {
			t.Fatalf("unexpected result %v", e)
		}
		if e != nil && strings.Contains(e.Error(), "secret") {
			t.Fatal("leaked value")
		}
		link := p + "-link"
		os.Symlink(p, link)
		if loadProviderEnv(link) == nil {
			t.Fatal("accepted symlink")
		}
	}
}
