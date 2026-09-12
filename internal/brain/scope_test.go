package brain

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestScopeUsesOriginAndPrincipalNotCredential(t *testing.T) {
	principal := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	a, err := ScopeFor("https://HOME.local:443/", principal)
	require.NoError(t, err)
	b, err := ScopeFor("https://home.local", principal)
	require.NoError(t, err)
	require.Equal(t, a, b)
	other, err := ScopeFor("https://home.local:8443", principal)
	require.NoError(t, err)
	require.NotEqual(t, a, other)
	other, err = ScopeFor("https://home.local", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	require.NoError(t, err)
	require.NotEqual(t, a, other)
	for _, origin := range []string{"http://home.local", "https://user:secret@home.local", "https://home.local/path", "https://home.local?x=1", "https://home.local/#x"} {
		_, err := ScopeFor(origin, principal)
		require.Error(t, err)
	}
	_, err = ScopeFor("https://home.local", "atr_int_secret")
	require.Error(t, err)
}

func TestScopeNormalizesIPv6AndNumericPort(t *testing.T) {
	principal := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	a, err := ScopeFor("https://[::1]:08443", principal)
	require.NoError(t, err)
	b, err := ScopeFor("https://[0:0:0:0:0:0:0:1]:8443", principal)
	require.NoError(t, err)
	require.Equal(t, a, b)
	_, err = ScopeFor("https://home.local:65536", principal)
	require.Error(t, err)
}
