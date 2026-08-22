FROM ubuntu:24.04@sha256:33ceb71981b602c1a7443a53469e4dba065f7503eab3078a2d7a57a2ab987517

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install --no-install-recommends -y ca-certificates git openssh-server \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --shell /bin/bash fixture \
    && passwd -d fixture \
    && install -d -m 0755 /run/sshd /workspace /hostkeys

COPY sshd_config /etc/ssh/sshd_config
COPY entrypoint.sh /usr/local/bin/pi-desk-sshd-entrypoint

EXPOSE 22
ENTRYPOINT ["/bin/sh", "/usr/local/bin/pi-desk-sshd-entrypoint"]
