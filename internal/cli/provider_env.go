package cli

import (
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"regexp"
	"strings"
)

// This is deliberately not a shell environment parser. Only one unquoted key
// is accepted; diagnostics never contain input or OS errors with secret data.
func loadProviderEnv(path string) error {
	if path == "" {
		return nil
	}
	fail := func() error {
		return fmt.Errorf("provider env: require an owned 0600 regular file containing one DEEPSEEK_API_KEY assignment")
	}
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if e != nil {
		return fail()
	}
	f := os.NewFile(uintptr(fd), "provider-env")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || int(st.Uid) != os.Geteuid() || st.Nlink != 1 || st.Size > 8192 {
		return fail()
	}
	b, e := io.ReadAll(io.LimitReader(f, 8193))
	if e != nil || len(b) > 8192 {
		return fail()
	}
	line := strings.TrimSuffix(string(b), "\n")
	if len(line) > 4113 || !regexp.MustCompile(`^DEEPSEEK_API_KEY=[A-Za-z0-9_./:+-]+$`).MatchString(line) {
		return fail()
	}
	return os.Setenv("DEEPSEEK_API_KEY", strings.TrimPrefix(line, "DEEPSEEK_API_KEY="))
}
