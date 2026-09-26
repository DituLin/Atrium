package integration

import "testing"

func TestTraceIdentifiersAreCanonicalAndBounded(t *testing.T) {
	for _, v := range []string{"", "private family text", "/private/nas", "01arz3ndektsv4rrffq69g5fav"} {
		if ValidTraceRef(v) {
			t.Fatalf("accepted %q", v)
		}
	}
	if !ValidTraceRef("01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Fatal("canonical ID rejected")
	}
}
