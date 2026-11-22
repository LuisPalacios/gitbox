#!/bin/sh
# Applies the per-test options of the gitbox-sshd container, then runs sshd.
#   AUTHORIZED_KEY  public key that may log in as "test"
#   LOGIN_SHELL     login shell of "test" (default /bin/bash)
#   BASHRC_B64      contents of ~test/.bashrc, base64-encoded
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

exec /usr/sbin/sshd -D -e
