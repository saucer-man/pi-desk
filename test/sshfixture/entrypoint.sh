#!/bin/sh
set -eu

if [ ! -s /keys/authorized_keys ]; then
    echo "fixture public key is missing" >&2
    exit 1
fi

install -d -m 0700 -o fixture -g fixture /home/fixture/.ssh
install -m 0600 -o fixture -g fixture /keys/authorized_keys /home/fixture/.ssh/authorized_keys
install -d -m 0755 -o fixture -g fixture /workspace/repo

if [ ! -f /hostkeys/ssh_host_ed25519_key ]; then
    ssh-keygen -q -t ed25519 -N '' -f /hostkeys/ssh_host_ed25519_key
fi
chmod 0600 /hostkeys/ssh_host_ed25519_key
chmod 0644 /hostkeys/ssh_host_ed25519_key.pub

if [ ! -d /workspace/repo/.git ]; then
    runuser -u fixture -- git -C /workspace/repo init -q
    runuser -u fixture -- git -C /workspace/repo config user.name "Pi Desk Fixture"
    runuser -u fixture -- git -C /workspace/repo config user.email "fixture@invalid"
fi

if [ "${PI_DESK_SSH_FIXTURE_MODE:-key}" = password ]; then
    printf 'fixture:pi-desk-fixture\n' | chpasswd
    set -- "$@" -o PubkeyAuthentication=no -o PasswordAuthentication=yes -o PermitEmptyPasswords=no
elif [ "${PI_DESK_SSH_FIXTURE_MODE:-key}" = bastion ]; then
    set -- "$@" -o AllowTcpForwarding=yes
elif [ "${PI_DESK_SSH_FIXTURE_MODE:-key}" = nohome ]; then
    rm -rf /home/fixture/.cache
    chmod 0500 /home/fixture
elif [ "${PI_DESK_SSH_FIXTURE_MODE:-key}" = readonly ]; then
    chmod 0555 /workspace/repo
fi

exec /usr/sbin/sshd -D -e -f /etc/ssh/sshd_config "$@"
