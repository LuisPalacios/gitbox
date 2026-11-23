#!/bin/sh
# Applies the per-test options of the gitbox-sshd container, then runs sshd.
#   AUTHORIZED_KEY  public key that may log in as "test"
#   LOGIN_SHELL     login shell of "test" (default /bin/bash)
#   BASHRC_B64      contents of ~test/.bashrc, base64-encoded
#   GIT_DAEMON      1 serves /srv/git read-only on git://127.0.0.1:9418
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

exec /usr/sbin/sshd -D -e
