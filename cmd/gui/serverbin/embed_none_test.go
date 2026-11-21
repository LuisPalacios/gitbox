//go:build !serverbin

package serverbin

import "testing"

// A build without the serverbin tag carries no server and needs none of the
// generated files: this test compiles and passes on a clean checkout.
func TestNoTagBuildsClean(t *testing.T) {
	if Version != "" || Commit != "" {
		t.Errorf("identity = %q %q, want none", Version, Commit)
	}
	if got := Targets(); len(got) != 0 {
		t.Errorf("Targets = %v, want none", got)
	}
	if _, ok := Lookup("linux", "amd64"); ok {
		t.Error("Lookup found a target")
	}
	if got, err := Binary(Target{OS: "linux", Arch: "amd64", Size: 1}); err == nil || got != nil {
		t.Errorf("Binary = %d bytes, %v; want an error", len(got), err)
	}
}
