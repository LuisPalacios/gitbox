# The sshd that the remote-mode integration tests connect to (#239):
# OpenSSH and fish on Debian slim, with a "test" user that logs in with a
# key only. scripts/remote-it.sh builds it as gitbox-sshd.
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends openssh-server fish \
    && rm -rf /var/lib/apt/lists/*

# The key lives outside the home folder, so a noexec tmpfs mounted over the
# home does not block the login. The account has no password and is not
# locked.
RUN useradd --create-home --uid 1000 --shell /bin/bash test \
    && usermod --password '*' test \
    && mkdir -p /run/sshd /etc/ssh/authorized_keys \
    && ssh-keygen -A

COPY sshd.conf /etc/ssh/sshd_config.d/gitbox.conf
COPY entrypoint.sh /entrypoint.sh
RUN chmod 0755 /entrypoint.sh

EXPOSE 22
ENTRYPOINT ["/entrypoint.sh"]
