// Command fakeforge is the provider of the token canary over ssh (#242):
// the Gitea API calls gitbox makes for a clone, a credential check and a
// mirror setup, and git's smart HTTP through git http-backend, both behind
// the tokens of a file. It runs inside the gitbox-sshd test container,
// whose network has no egress, and keeps nothing but bare repositories.
//
//	fakeforge -listen 127.0.0.1:3000 -root /tmp/forge -tokens /tmp/forge-tokens
//
// The tokens file holds one "user token" pair per line. It is a file, not
// a flag, so no token is on the command line of a process of the host,
// which the canary test watches.
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type forge struct {
	root   string
	git    string
	tokens map[string]string // token -> user
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func main() {
	listen := flag.String("listen", "127.0.0.1:3000", "address to serve")
	root := flag.String("root", "/tmp/forge", "folder of the bare repositories, <owner>/<repo>.git")
	tokensFile := flag.String("tokens", "", "file of \"user token\" lines")
	flag.Parse()
	git, err := exec.LookPath("git")
	if err != nil {
		log.Fatal(err)
	}
	f := &forge{root: *root, git: git, tokens: map[string]string{}}
	file, err := os.Open(*tokensFile)
	if err != nil {
		log.Fatal(err)
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		user, token, ok := strings.Cut(strings.TrimSpace(scanner.Text()), " ")
		if ok {
			f.tokens[token] = user
		}
	}
	_ = file.Close()
	log.Fatal(http.ListenAndServe(*listen, f))
}

// user is who the request authenticates as: "token X", "Bearer X" or Basic
// with X as the password. Empty when it does not.
func (f *forge) user(r *http.Request) string {
	h := r.Header.Get("Authorization")
	for _, prefix := range []string{"token ", "Bearer "} {
		if v, ok := strings.CutPrefix(h, prefix); ok {
			return f.tokens[v]
		}
	}
	if v, ok := strings.CutPrefix(h, "Basic "); ok {
		raw, err := base64.StdEncoding.DecodeString(v)
		if err == nil {
			if _, pass, ok := strings.Cut(string(raw), ":"); ok {
				return f.tokens[pass]
			}
		}
	}
	return ""
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *forge) repoPath(owner, repo string) (string, bool) {
	repo = strings.TrimSuffix(repo, ".git")
	if !safeName.MatchString(owner) || !safeName.MatchString(repo) {
		return "", false
	}
	return filepath.Join(f.root, owner, repo+".git"), true
}

func (f *forge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user := f.user(r)
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if user == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="fakeforge"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		f.serveGit(w, r, user)
		return
	}
	if user == "" {
		reply(w, http.StatusUnauthorized, map[string]string{"message": "token is required"})
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1"), "/"), "/")
	switch {
	case r.Method == http.MethodGet && len(parts) == 1 && parts[0] == "user":
		reply(w, http.StatusOK, map[string]string{"login": user, "username": user})
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "user":
		reply(w, http.StatusOK, []any{})
	case r.Method == http.MethodGet && len(parts) >= 3 && parts[0] == "repos" && parts[1] != "issues":
		f.serveRepo(w, parts[1], parts[2], parts[3:])
	case r.Method == http.MethodGet:
		reply(w, http.StatusOK, []any{})
	case r.Method == http.MethodPost && (len(parts) == 2 && parts[0] == "user" && parts[1] == "repos" || len(parts) == 3 && parts[0] == "orgs"):
		owner := user
		if parts[0] == "orgs" {
			owner = parts[1]
		}
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.create(w, owner, body.Name)
	case r.Method == http.MethodPost && len(parts) == 2 && parts[0] == "repos" && parts[1] == "migrate":
		reply(w, http.StatusCreated, map[string]string{})
	case r.Method == http.MethodPost:
		reply(w, http.StatusCreated, map[string]string{})
	default:
		reply(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}
}

func (f *forge) serveRepo(w http.ResponseWriter, owner, repo string, rest []string) {
	path, ok := f.repoPath(owner, repo)
	if _, err := os.Stat(path); !ok || err != nil {
		reply(w, http.StatusNotFound, map[string]string{"message": "not found"})
		return
	}
	if len(rest) >= 2 && rest[0] == "branches" {
		reply(w, http.StatusOK, map[string]any{"name": rest[1], "commit": map[string]string{"id": "0"}})
		return
	}
	if len(rest) > 0 {
		reply(w, http.StatusOK, []any{})
		return
	}
	reply(w, http.StatusOK, map[string]any{"full_name": owner + "/" + repo, "default_branch": "main", "private": true})
}

func (f *forge) create(w http.ResponseWriter, owner, name string) {
	path, ok := f.repoPath(owner, name)
	if !ok {
		reply(w, http.StatusUnprocessableEntity, map[string]string{"message": "bad name"})
		return
	}
	if _, err := os.Stat(path); err == nil {
		reply(w, http.StatusConflict, map[string]string{"message": "exists"})
		return
	}
	if out, err := exec.Command(f.git, "init", "-q", "--bare", "-b", "main", path).CombinedOutput(); err != nil {
		reply(w, http.StatusInternalServerError, map[string]string{"message": fmt.Sprintf("%v %s", err, out)})
		return
	}
	reply(w, http.StatusCreated, map[string]string{"full_name": owner + "/" + name})
}

// serveGit hands a smart HTTP request to git http-backend. The credentials
// stay here: the backend gets the user, not the Authorization header.
func (f *forge) serveGit(w http.ResponseWriter, r *http.Request, user string) {
	r.Header.Del("Authorization")
	h := &cgi.Handler{
		Path: f.git,
		Args: []string{"http-backend"},
		Env:  []string{"GIT_PROJECT_ROOT=" + f.root, "GIT_HTTP_EXPORT_ALL=1", "REMOTE_USER=" + user},
	}
	h.ServeHTTP(w, r)
}
