package config

import (
	"reflect"
	"testing"
)

func TestIgnoreClone(t *testing.T) {
	c := rootsConfig()
	if !c.IgnoreClone("/r1/scratch/thing", true) || c.IgnoreClone("/r1/scratch/thing/", true) {
		t.Fatal("first ignore must change the list, an equivalent path must not")
	}
	if !c.IsIgnoredClone("/R1/scratch/thing") {
		t.Error("ignored path not recognised")
	}
	if !c.IgnoreClone("/r1/scratch/thing", false) || len(c.Global.IgnoredClones) != 0 {
		t.Errorf("stop ignoring: %v", c.Global.IgnoredClones)
	}
}

func TestConfigClone_IsDeepAndKeepsOrder(t *testing.T) {
	c := rootsConfig()
	c.SourceOrder = []string{"two", "one"}
	src := c.Sources["one"]
	src.RepoOrder = []string{"org/y", "org/x"}
	c.Sources["one"] = src

	cp, err := c.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cp, c) {
		t.Fatalf("clone differs:\n%+v\n%+v", cp, c)
	}
	cp.Global.Roots[0] = "/changed"
	cp.Sources["one"].Repos["org/x"] = Repo{Root: "/changed"}
	if c.Global.Roots[0] != "/r1" || c.Sources["one"].Repos["org/x"].Root != "" {
		t.Error("clone shares state with the original")
	}
}
