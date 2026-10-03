//go:build darwin || linux

package archive

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNativeReapsDescendantsOnCancelAndPipeDelay(t *testing.T) {
	for _, mode := range []string{"cancel", "leader_exits"} {
		t.Run(mode, func(t *testing.T) {
			pidfile := filepath.Join(t.TempDir(), "pid")
			body := "sleep 30 &\necho $! > '" + pidfile + "'\n"
			if mode == "cancel" {
				body += "wait\n"
			} else {
				body += "exit 0\n"
			}
			p := NativeParser{HelperPath: nativeScript(t, body), Timeout: time.Second}
			if mode == "leader_exits" {
				p.Timeout = 3 * time.Second
			}
			_, err := p.parseProcess(context.Background(), nativeOriginal(t))
			if err == nil {
				t.Fatal("accepted fixture")
			}
			b, err := os.ReadFile(pidfile)
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.Kill(pid, syscall.SIGKILL)
			until := time.Now().Add(2 * time.Second)
			for time.Now().Before(until) {
				if syscall.Kill(pid, 0) == syscall.ESRCH {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatalf("descendant %d survived parser return", pid)
		})
	}
}
