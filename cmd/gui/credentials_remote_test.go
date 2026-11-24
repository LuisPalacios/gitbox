package main

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// TestHostRefusesGCM: a host refuses the GCM sign-in and a switch to GCM
// with gcm_unsupported_remote and a reason that names the store GCM would
// use there, without starting git (#242). The token methods go through.
func TestHostRefusesGCM(t *testing.T) {
	path := contractHost(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	argv := &argvRecorder{}
	argv.install(t)
	link := serveRemote(t, path, nil)
	before := loadConfig(t, path).Accounts["acct"]

	res := link.Remote.CredentialSetupGCM("acct")
	ue := toUserError(link.Remote.LastError())
	if res.OK || ue.Code != "gcm_unsupported_remote" || ue.Hint != "error.gcm_unsupported_remote."+runtime.GOOS {
		t.Fatalf("CredentialSetupGCM = %+v, error %+v; want gcm_unsupported_remote with this OS's hint", res, ue)
	}
	if res.Message == "" || !strings.Contains(res.Message, gcmStores[runtime.GOOS]) {
		t.Errorf("the window's message %q does not name the store %q", res.Message, gcmStores[runtime.GOOS])
	}
	if _, err := link.Remote.ChangeCredentialType("acct", "gcm", ""); toUserError(err).Code != "gcm_unsupported_remote" {
		t.Errorf("ChangeCredentialType(gcm) = %v, want gcm_unsupported_remote", err)
	}
	if after := loadConfig(t, path).Accounts["acct"]; !reflect.DeepEqual(after, before) {
		t.Errorf("a refused GCM call changed the account: %+v, was %+v", after, before)
	}
	if strings.Contains(argv.String(), "credential") {
		t.Errorf("a refused GCM call started git credential:\n%s", argv)
	}

	// The token methods are served.
	if res := link.Remote.CredentialStoreToken("acct", "a-token-value"); !res.OK {
		t.Errorf("CredentialStoreToken = %+v (%v)", res, link.Remote.LastError())
	}
	if res := link.Remote.CredentialDelete("acct"); !res.OK {
		t.Errorf("CredentialDelete = %+v (%v)", res, link.Remote.LastError())
	}
}

// TestGCMReasonNamesTheStore: each OS's reason names its store and never
// says GCM cannot work on any remote.
func TestGCMReasonNamesTheStore(t *testing.T) {
	for _, os := range []string{"linux", "darwin", "windows", "plan9"} {
		e := &gcmRemoteError{OS: os}
		ue := toUserError(e)
		if ue.Code != "gcm_unsupported_remote" || identifier(ue.Hint) != ue.Hint || ue.Hint == "" {
			t.Errorf("%s: %+v", os, ue)
		}
		if store, ok := gcmStores[os]; ok && !strings.Contains(e.Error(), store) {
			t.Errorf("%s: %q does not name %q", os, e.Error(), store)
		}
		if strings.Contains(strings.ToLower(e.Error()), "any remote") {
			t.Errorf("%s: %q claims too much", os, e.Error())
		}
	}
}
