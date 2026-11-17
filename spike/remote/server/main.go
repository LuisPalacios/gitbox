// Command server is the throwaway P0 spike server for remote mode (#234).
// It speaks LSP-framed JSON-RPC 2.0 on stdin/stdout after a magic-byte
// handshake. It links the real pkg/ libraries so its size is representative
// of the future gitbox-server. Never merged.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/doctor"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/gitignore"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/identity"
	"github.com/LuisPalacios/gitbox/pkg/mirror"
	"github.com/LuisPalacios/gitbox/pkg/move"
	"github.com/LuisPalacios/gitbox/pkg/ops"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/LuisPalacios/gitbox/pkg/reconcile"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/LuisPalacios/gitbox/pkg/workspace"
)

// Magic is written before any frame; deliberately not the gzip header.
var Magic = []byte{0x00, 0xFE, 'g', 'b'}

const maxFrame = 16 << 20

var version = "spike"

// linked keeps the real library surface reachable so the binary size is
// representative. It is printed by -linked so the linker cannot drop it.
var linked = map[string]any{
	"ops.PlanClone": ops.PlanClone, "ops.AddAccount": ops.AddAccount, "ops.ListRemoteRepos": ops.ListRemoteRepos,
	"ops.ChangeCredentialType": ops.ChangeCredentialType, "ops.DeleteAccount": ops.DeleteAccount,
	"git.PullQuiet": git.PullQuiet, "git.DetailedStatus": git.DetailedStatus, "git.SweepBranches": git.SweepBranches,
	"credential.StoreToken": credential.StoreToken, "credential.CheckContext": credential.CheckContext,
	"provider.ByName": provider.ByName, "mirror.DiscoverMirrors": mirror.DiscoverMirrors, "mirror.SetupMirror": mirror.SetupMirror,
	"move.Move": move.Move, "move.Preflight": move.Preflight, "reconcile.Plan": reconcile.Plan, "reconcile.Apply": reconcile.Apply,
	"workspace.Sync": workspace.Sync, "adopt.FindOrphans": adopt.FindOrphans, "heal.Repo": heal.Repo,
	"identity.CheckGlobalIdentity": identity.CheckGlobalIdentity, "gitignore.Check": gitignore.Check,
	"status.ComputeNesting": status.ComputeNesting,
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// StatusResult mirrors cmd/gui StatusResult JSON tags.
type StatusResult struct {
	Source    string `json:"source"`
	Repo      string `json:"repo"`
	Account   string `json:"account"`
	Path      string `json:"path"`
	State     string `json:"state"`
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
	Modified  int    `json:"modified"`
	Untracked int    `json:"untracked"`
	Conflicts int    `json:"conflicts"`
	Error     string `json:"error,omitempty"`
	Branch    string `json:"branch,omitempty"`
	IsDefault bool   `json:"isDefault,omitempty"`
}

func writeFrame(w *bufio.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(b)); err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	return w.Flush()
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	n := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
			n, err = strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return nil, err
			}
		}
	}
	if n < 0 || n > maxFrame {
		return nil, fmt.Errorf("bad frame length %d", n)
	}
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

func synthetic(n int) []StatusResult {
	states := []string{"clean", "behind", "ahead", "dirty", "diverged", "not cloned"}
	out := make([]StatusResult, n)
	for i := range out {
		out[i] = StatusResult{
			Source: fmt.Sprintf("account-%02d", i%12), Repo: fmt.Sprintf("org-%02d/repository-name-%03d", i%20, i),
			Account: fmt.Sprintf("account-%02d", i%12), Path: fmt.Sprintf("/home/user/src/account-%02d/org-%02d/repository-name-%03d", i%12, i%20, i),
			State: states[i%len(states)], Ahead: i % 3, Behind: i % 5, Modified: i % 7, Untracked: i % 4,
			Branch: "main", IsDefault: i%9 != 0,
		}
	}
	return out
}

func handle(method string, params json.RawMessage) (any, *rpcErr) {
	switch method {
	case "ping":
		return "pong", nil
	case "status":
		var p []int
		_ = json.Unmarshal(params, &p)
		n := 300
		if len(p) == 1 {
			n = p[0]
		}
		return synthetic(n), nil
	case "realstatus":
		cfg, err := config.Load(config.DefaultV2Path())
		if err != nil {
			return nil, &rpcErr{Code: -32000, Message: "no config on this host"}
		}
		rs := status.CheckAll(cfg)
		return map[string]int{"repos": len(rs)}, nil
	case "doctor":
		res := doctor.Check(doctor.StandardTools())
		return map[string]any{"tools": len(res), "credFindings": len(credential.AuditCredentialDir())}, nil
	}
	return nil, &rpcErr{Code: -32601, Message: "method not found"}
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version":
			fmt.Printf("%s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
			return
		case "-linked":
			fmt.Println(len(linked))
			return
		}
	}
	w := bufio.NewWriterSize(os.Stdout, 64<<10)
	r := bufio.NewReaderSize(os.Stdin, 64<<10)
	if _, err := w.Write(Magic); err != nil {
		os.Exit(1)
	}
	if err := writeFrame(w, map[string]any{"version": version, "os": runtime.GOOS, "arch": runtime.GOARCH, "pid": os.Getpid()}); err != nil {
		os.Exit(1)
	}
	for {
		b, err := readFrame(r)
		if err != nil {
			return // stdin closed: exit
		}
		var m rpcMsg
		if json.Unmarshal(b, &m) != nil || m.ID == nil {
			continue
		}
		res, rerr := handle(m.Method, m.Params)
		if err := writeFrame(w, rpcMsg{JSONRPC: "2.0", ID: m.ID, Result: res, Error: rerr}); err != nil {
			return
		}
	}
}
