#!/usr/bin/env bash
# Create a reproducible local fleet in an empty directory.
set -euo pipefail
case "${1:-}" in 50|200|500) ;; *) echo "usage: benchmark.sh {50|200|500} EMPTY_DIRECTORY" >&2; exit 2;; esac
[[ $# == 2 ]] || exit 2
mkdir -p "$2"
root="$(cd "$2" && pwd -P)"
[[ -z "$(ls -A "$root")" ]] || { echo "benchmark directory must be empty" >&2; exit 1; }
mkdir -p "$root/config/gitbox" "$root/fleet/demo/bench" "$root/home"
export HOME="$root/home" GIT_CONFIG_GLOBAL="$root/gitconfig" GIT_CONFIG_NOSYSTEM=1
git init -q -b main "$root/seed"
echo benchmark > "$root/seed/README"
git -C "$root/seed" add README
git -C "$root/seed" -c user.name=Fixture -c user.email=fixture@example.invalid commit -qm initial
git clone -q --bare "$root/seed" "$root/origin.git"
for ((i=1; i<=$1; i++)); do
    printf -v repo 'project-%03d' "$i"
    dest="$root/fleet/demo/bench/$repo"
    git clone -q --local "$root/origin.git" "$dest"
    git -C "$dest" remote set-url origin "http://127.0.0.1:1/bench/$repo.git"
done
python3 - "$root" "$1" <<'PY'
import json
import sys
from pathlib import Path

root = Path(sys.argv[1]).resolve()
config = {
    "version": 5,
    "global": {"roots": [str(root / "fleet")], "collapsed": {"view": "all"},
               "ui_mode": "advanced", "periodic_sync": "off", "language": "en"},
    "accounts": {"demo": {"provider": "gitea", "url": "http://127.0.0.1:1",
                          "username": "bench", "name": "Fixture", "email": "fixture@example.invalid",
                          "default_credential_type": "token"}},
    "sources": {"demo": {"account": "demo", "repos": {
        f"bench/project-{i:03d}": {} for i in range(1, int(sys.argv[2]) + 1)}}},
}
path = root / "config/gitbox/gitbox.json"
path.write_text(json.dumps(config, indent=2) + "\n", encoding="utf-8")
print(path)
PY
