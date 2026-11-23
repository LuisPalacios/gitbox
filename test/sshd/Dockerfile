# The sshd that the remote-mode integration tests connect to (#239):
# OpenSSH, fish and git on Debian slim, with a "test" user that logs in with
# a key only. git lets the session tests (#240) check real clones; iproute2
# lets the fault tests (#295) shape the network with tc netem, given
# --cap-add NET_ADMIN, and procps lets them look for git processes. A "git"
# user whose shell is git-shell lets a second container act as the git host
# of an SSH account (#244).
# scripts/remote-it.sh builds it as gitbox-sshd.
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends openssh-server fish git iproute2 procps \
    && rm -rf /var/lib/apt/lists/*

# A fixture repository of 60 MB that does not compress, served read-only by
# git daemon on 127.0.0.1:9418 when the container starts with GIT_DAEMON=1:
# big enough that a clone is still running when a test cuts it.
RUN git init -q -b main /tmp/fixture \
    && for i in 1 2 3 4 5 6; do \
         head -c 10M /dev/urandom > /tmp/fixture/blob$i \
         && git -C /tmp/fixture add blob$i \
         && git -C /tmp/fixture -c user.name=fixture -c user.email=f@example.invalid commit -q -m "blob $i"; \
       done \
    && git clone -q --bare /tmp/fixture /srv/git/org/fixture.git \
    && rm -rf /tmp/fixture \
    && chmod -R a+rX /srv/git

# The key lives outside the home folder, so a noexec tmpfs mounted over the
# home does not block the login. The account has no password and is not
# locked. pam_motd is off: sshd would write /run/motd.dynamic on every
# login, which the noexec test counts as a file written outside the home.
RUN useradd --create-home --uid 1000 --shell /bin/bash test \
    && usermod --password '*' test \
    && useradd --create-home --home-dir /srv/githost --uid 1001 --shell /usr/bin/git-shell git \
    && usermod --password '*' git \
    && mkdir -p /run/sshd /etc/ssh/authorized_keys \
    && ssh-keygen -A \
    && sed -i '/pam_motd/d' /etc/pam.d/sshd

COPY sshd.conf /etc/ssh/sshd_config.d/gitbox.conf
COPY entrypoint.sh /entrypoint.sh
RUN chmod 0755 /entrypoint.sh

EXPOSE 22
ENTRYPOINT ["/entrypoint.sh"]
