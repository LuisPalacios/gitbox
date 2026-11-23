package rpc

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const canary = "gbx-canary-0123456789abcdef0123456789abcdef"

var testSecrets = SecretParams{"Store": {1}}

// TestSecretParamsRedacted: the token of a secret request leaves neither end
// again. The server withholds a panic value that holds it and removes it
// from its answer, success or error; the client removes it from an answer
// of a server that does not (#242).
func TestSecretParamsRedacted(t *testing.T) {
	serverLog, clientLog := &syncBuffer{}, &syncBuffer{}
	mode := ""
	handlers := map[string]Handler{
		"Store": Func(func(key, token string) (map[string]string, error) {
			switch mode {
			case "panic":
				panic("cannot store " + token)
			case "error":
				return nil, errors.New("the provider refused " + token + " for " + key)
			}
			return map[string]string{"message": "stored " + token}, nil
		}),
	}
	srv := newServer(handlers)
	srv.Log = serverLog
	srv.SecretParams = testSecrets
	l, err := tryConnect(t, srv, ClientOptions{Version: testVersion, Commit: testCommit, Log: clientLog, SecretParams: testSecrets})
	if err != nil {
		t.Fatal(err)
	}
	var answers []string
	for _, m := range []string{"ok", "error", "panic"} {
		mode = m
		var out map[string]string
		err := l.call("Store", []any{"acct", canary}, &out)
		answers = append(answers, m+": "+asText(out)+" / "+errText(err))
	}
	all := strings.Join(answers, "\n")
	for where, text := range map[string]string{"answers": all, "server log": serverLog.String(), "client log": clientLog.String()} {
		if strings.Contains(text, canary) {
			t.Errorf("the token is in the %s:\n%s", where, text)
		}
	}
	// Positive control: the calls really ran, and the panic was logged.
	if !strings.Contains(all, "stored [secret]") || !strings.Contains(all, "refused [secret] for acct") {
		t.Errorf("answers = %s; want the handler's words with the token replaced", all)
	}
	if !strings.Contains(serverLog.String(), "panic in Store: [secret]") {
		t.Errorf("server log = %q; want the panic with its value withheld", serverLog.String())
	}

	// A server that does not mark the secret echoes it; the client still
	// removes it.
	mode = ""
	naive := newServer(handlers)
	l2 := connect(t, naive)
	var out map[string]string
	if err := l2.call("Store", []any{"acct", canary}, &out); err != nil || !strings.Contains(out["message"], canary) {
		t.Fatalf("positive control: a client without the marker gets the echo, got %v %v", out, err)
	}
	guarded, err := tryConnect(t, newServer(handlers), ClientOptions{Version: testVersion, Commit: testCommit, SecretParams: testSecrets})
	if err != nil {
		t.Fatal(err)
	}
	out = nil
	if err := guarded.call("Store", []any{"acct", canary}, &out); err != nil || out["message"] != "stored [secret]" {
		t.Errorf("a marked client decoded %v (%v); want the token replaced", out, err)
	}
}

func TestRedactKeepsArity(t *testing.T) {
	for _, tc := range []struct{ method, in, want string }{
		{"Store", `["acct","` + canary + `"]`, `["acct","[secret]"]`},
		{"Store", `["acct"]`, `["acct"]`},
		{"Store", `{"token":"` + canary + `"}`, `"[secret]"`},
		{"Other", `["acct","` + canary + `"]`, `["acct","` + canary + `"]`},
	} {
		if got := string(testSecrets.Redact(tc.method, json.RawMessage(tc.in))); got != tc.want {
			t.Errorf("Redact(%s, %s) = %s, want %s", tc.method, tc.in, got, tc.want)
		}
	}
}

func asText(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
