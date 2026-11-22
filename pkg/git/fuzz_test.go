package git

import (
	"slices"
	"strings"
	"testing"
)

func FuzzStatusRecords(f *testing.F) {
	f.Add("a.txt")
	f.Add(" -file\n\".txt ")
	f.Add("file\x00other")
	f.Add("2 R. N... 100644 100644 100644 a b R100 new\x00old\x00")
	f.Fuzz(func(t *testing.T, input string) {
		if parsed, err := parseStatusZ(input); err == nil {
			_ = parsed.summary()
			_, _ = parsed.details()
		}

		path := "file-" + strings.ReplaceAll(input, "\x00", "")
		oldPath := "old-" + path
		records := "2 R. N... 100644 100644 100644 a b R100 " + path + "\x00" + oldPath + "\x00" +
			"1 .M N... 100644 100644 100644 a b " + path + "\x00" +
			"u UU N... 100644 100644 100644 100644 a b c " + path + "\x00" +
			"? " + path + "\x00"
		parsed, err := parseStatusZ(records)
		if err != nil {
			t.Fatal(err)
		}
		if want := (RepoStatus{Added: 1, Modified: 1, Conflicts: 1, Untracked: 1}); parsed.summary() != want {
			t.Errorf("summary = %+v, want %+v", parsed.summary(), want)
		}
		changed, untracked := parsed.details()
		want := []FileChange{{Kind: "renamed", Path: oldPath + " → " + path}, {Kind: "modified", Path: path}, {Kind: "conflict", Path: path}}
		if !slices.Equal(changed, want) || !slices.Equal(untracked, []string{path}) {
			t.Errorf("details did not preserve the generated paths and change kinds")
		}
	})
}
