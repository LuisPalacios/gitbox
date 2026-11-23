#!/bin/sh
# Applies the per-test options of the gitbox-sshd container, then runs sshd.
#   AUTHORIZED_KEY  public key that may log in as "test"
#   LOGIN_SHELL     login shell of "test" (default /bin/bash)
#   BASHRC_B64      contents of ~test/.bashrc, base64-encoded
#   GIT_DAEMON      1 serves /srv/git read-only on git://127.0.0.1:9418
#   GIT_HOST        1 makes this container the git host of an SSH account:
#                   the "git" user (git-shell) owns ~git/org/small.git; a
#                   test installs the key that may log in as git
set -eu

if [ -n "${AUTHORIZED_KEY:-}" ]; then
    printf '%s\n' "$AUTHORIZED_KEY" > /etc/ssh/authorized_keys/test
    chmod 0644 /etc/ssh/authorized_keys/test
fi
if [ -n "${LOGIN_SHELL:-}" ]; then
    chsh -s "$LOGIN_SHELL" test
fi
# A tmpfs over the home starts empty.
mkdir -p /home/test
chown test:test /home/test
if [ -n "${BASHRC_B64:-}" ]; then
    printf '%s' "$BASHRC_B64" | base64 -d > /home/test/.bashrc
    chown test:test /home/test/.bashrc
fi

if [ "${GIT_DAEMON:-}" = 1 ]; then
    git daemon --base-path=/srv/git --export-all --reuseaddr \
        --listen=127.0.0.1 --port=9418 --detach
fi

if [ "${GIT_HOST:-}" = 1 ]; then
    git init -q -b main /tmp/small
    git -C /tmp/small -c user.name=fixture -c user.email=f@example.invalid commit -q --allow-empty -m one
    mkdir -p /srv/githost/org
    git clone -q --bare /tmp/small /srv/githost/org/small.git
    rm -rf /tmp/small
    chown -R git:git /srv/githost
fi

exec /usr/sbin/sshd -D -e
