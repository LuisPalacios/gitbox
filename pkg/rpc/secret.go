package rpc

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/usererr"
)

// SecretParams marks, per method, the positions of the parameters that carry
// a secret, such as the token of CredentialStoreToken (#242). A secret goes
// from the client to the server once, in its request, and never comes back:
// both ends remove its value from whatever they log or answer about that
// request.
//
// Neither end logs request parameters today, and there is no request trace.
// A trace or log line added later must log Redact's output, never the raw
// parameters.
type SecretParams map[string][]int

// secretMark replaces a secret wherever it would otherwise appear.
const secretMark = "[secret]"

// Redact returns params with every secret position replaced by "[secret]",
// keeping the arity. Parameters that are not a JSON array are replaced
// whole. A method without secrets gets params back unchanged.
func (s SecretParams) Redact(method string, params json.RawMessage) json.RawMessage {
	positions := s[method]
	if len(positions) == 0 {
		return params
	}
	var items []json.RawMessage
	if err := json.Unmarshal(params, &items); err != nil {
		return json.RawMessage(`"` + secretMark + `"`)
	}
	for _, i := range positions {
		if i >= 0 && i < len(items) {
			items[i] = json.RawMessage(`"` + secretMark + `"`)
		}
	}
	out, err := json.Marshal(items)
	if err != nil {
		return json.RawMessage(`"` + secretMark + `"`)
	}
	return out
}

// values returns the secret strings a request of method carries.
func (s SecretParams) values(method string, params json.RawMessage) []string {
	positions := s[method]
	if len(positions) == 0 {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(params, &items); err != nil {
		return nil
	}
	var out []string
	for _, i := range positions {
		if i < 0 || i >= len(items) {
			continue
		}
		var v string
		if json.Unmarshal(items[i], &v) == nil {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// scrubText removes every secret from s.
func scrubText(s string, secrets []string) string {
	for _, v := range secrets {
		s = strings.ReplaceAll(s, v, secretMark)
	}
	return s
}

// scrubUserError removes every secret from the words of ue.
func scrubUserError(ue usererr.UserError, secrets []string) usererr.UserError {
	ue.Code = scrubText(ue.Code, secrets)
	ue.Hint = scrubText(ue.Hint, secrets)
	ue.Detail = scrubText(ue.Detail, secrets)
	return ue
}

// scrubJSON removes every secret from an encoded value, in the escaped form
// it takes inside a JSON string.
func scrubJSON(data json.RawMessage, secrets []string) json.RawMessage {
	for _, v := range secrets {
		quoted, err := json.Marshal(v)
		if err != nil || len(quoted) < 2 {
			continue
		}
		data = bytes.ReplaceAll(data, quoted[1:len(quoted)-1], []byte(secretMark))
	}
	return data
}
