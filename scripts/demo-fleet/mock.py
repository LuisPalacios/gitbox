#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Mock provider API for the gitbox demo fleet (see build.sh).

Usage: DEMO_ROOT=<dir> uv run scripts/demo-fleet/mock.py   (run.sh does this)

Three listeners, one per fake account:
  3001  Forgejo   (/api/v1)  user "demo"
  3002  GitHub    (/api/v3)  user "alexdev"
  3003  GitHub    (/api/v3)  user "alex-acme"
Every request is logged so unknown endpoints show up and can be added.
Unknown GETs answer [] so list endpoints degrade to "nothing".
Paths containing ".git/" are served as static files from the bare
upstreams (git dumb-HTTP), so fetches keep behind/ahead counts real.
"""
import json, os, sys, threading, zlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

ACCOUNTS = {
    3001: {"kind": "forgejo", "login": "demo", "repos": ["infra/ansible", "infra/k8s-cluster", "tools/dotfiles", "docs/wiki"]},
    3002: {"kind": "github", "login": "alexdev", "repos": ["alexdev/blog", "alexdev/cli-tools", "alexdev/notes", "alexdev/raytracer"]},
    3003: {"kind": "github", "login": "alex-acme", "repos": ["acme-corp/api-gateway", "acme-corp/web-portal", "acme-corp/payments"]},
}

if not os.environ.get("DEMO_ROOT"):
    sys.exit("mock.py: DEMO_ROOT is not set (start it through run.sh)")
UPSTREAMS = os.path.join(os.environ["DEMO_ROOT"], "upstreams")
ACCOUNT_KEYS = {3001: "forge-homelab", 3002: "github-alexdev", 3003: "github-acme"}

# Open PRs authored by the user / review requests, keyed by "owner/repo".
PRS = {
    3003: {"authored": {"acme-corp/api-gateway": 2}, "review": {"acme-corp/payments": 1}},
    3002: {"authored": {"alexdev/cli-tools": 1}, "review": {}},
    3001: {"authored": {}, "review": {"infra/k8s-cluster": 1}},
}


def repo_obj(port, full):
    owner, name = full.split("/", 1)
    base = f"http://127.0.0.1:{port}"
    return {
        "id": zlib.crc32(full.encode()) % 100000, "name": name, "full_name": full,
        "owner": {"login": owner, "username": owner},
        "private": False, "fork": False, "archived": False, "empty": False,
        "default_branch": "main", "description": f"{name} (demo)",
        "html_url": f"{base}/{full}", "clone_url": f"{base}/{full}.git",
        "ssh_url": f"git@127.0.0.1:{full}.git",
        "permissions": {"admin": True, "push": True, "pull": True},
    }


def pr_items(port, which):
    out, n = [], 1
    for full, count in PRS[port][which].items():
        for _ in range(count):
            out.append({
                "number": n, "title": f"Demo change #{n}", "state": "open", "draft": False,
                "html_url": f"http://127.0.0.1:{port}/{full}/pulls/{n}",
                "repository_url": f"http://127.0.0.1:{port}/api/v3/repos/{full}",
                "repository": {"full_name": full, "owner": full.split('/')[0], "name": full.split('/')[1]},
                "user": {"login": ACCOUNTS[port]["login"]},
                "pull_request": {"html_url": f"http://127.0.0.1:{port}/{full}/pulls/{n}"},
                "created_at": "2026-09-28T10:00:00Z", "updated_at": "2026-10-02T10:00:00Z",
            })
            n += 1
    return out


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        sys.stderr.write(f"[{self.server.server_port}] {self.command} {self.path} -> {getattr(self, '_code', '?')}\n")

    def send(self, code, body, headers=None):
        self._code = code
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        if self.server.server_port != 3001:
            self.send_header("X-OAuth-Scopes", "repo, read:org, workflow")
        for k, v in (headers or {}).items():
            self.send_header(k, v)
        self.end_headers()
        self.wfile.write(data)

    def serve_git(self, port, path):
        # Dumb-HTTP git: map /<org>/<repo>.git/... onto the bare upstream.
        rel = path.lstrip("/")
        f = os.path.join(UPSTREAMS, ACCOUNT_KEYS[port], *rel.split("/"))
        if not os.path.isfile(f):
            self._code = 404
            self.send_response(404); self.send_header("Content-Length", "0"); self.end_headers()
            return
        data = open(f, "rb").read()
        self._code = 200
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        port = self.server.server_port
        acct = ACCOUNTS[port]
        u = urlparse(self.path)
        if ".git/" in u.path:
            return self.serve_git(port, u.path)
        p = u.path.rstrip("/")
        q = parse_qs(u.query)
        for pre in ("/api/v1", "/api/v3"):
            if p.startswith(pre):
                p = p[len(pre):]
        if p == "/user":
            return self.send(200, {"login": acct["login"], "username": acct["login"], "id": port, "name": "Alex Demo", "email": f"{acct['login']}@example.com"})
        if p in ("/user/repos", "/user/repos/search", "/repos/search"):
            page = int(q.get("page", ["1"])[0])
            repos = [repo_obj(port, r) for r in acct["repos"]] if page == 1 else []
            return self.send(200, {"ok": True, "data": repos} if p == "/repos/search" else repos)
        if p in ("/user/orgs", "/user/memberships/orgs"):
            orgs = sorted({r.split("/")[0] for r in acct["repos"]} - {acct["login"]})
            return self.send(200, [{"login": o, "username": o, "name": o} for o in orgs])
        if p == "/search/issues":
            query = q.get("q", [""])[0]
            which = "review" if "review-requested" in query else "authored"
            items = pr_items(port, which)
            return self.send(200, {"total_count": len(items), "incomplete_results": False, "items": items})
        if p == "/repos/issues/search":
            which = "review" if q.get("review_requested") or "review" in u.query else "authored"
            return self.send(200, pr_items(port, which))
        if p.startswith("/repos/"):
            parts = p.split("/")
            if len(parts) >= 4:
                full = f"{parts[2]}/{parts[3]}"
                if len(parts) == 4:
                    return self.send(200, repo_obj(port, full))
                if parts[4] == "branches":
                    return self.send(200, {"name": "main", "commit": {"id": "0" * 40, "sha": "0" * 40}})
                if parts[4] in ("pulls", "push_mirrors"):
                    return self.send(200, [])
        return self.send(200, [])

    do_HEAD = do_GET


def serve(port):
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()


if __name__ == "__main__":
    for port in ACCOUNTS:
        threading.Thread(target=serve, args=(port,), daemon=True).start()
    sys.stderr.write("mock API on 3001 (forgejo), 3002/3003 (github)\n")
    threading.Event().wait()
