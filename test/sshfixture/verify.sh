#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
fixture="$root/test/sshfixture/manage.sh"
temporary=$(mktemp -d)
original_gocache=$(go env GOCACHE)
original_gomodcache=$(go env GOMODCACHE)
build_identity=pi-desk-v1.0.1-p1
helper="$root/build/remote-helper/artifacts/helper-linux-amd64"

cleanup() {
    sh "$fixture" stop >/dev/null 2>&1 || true
    rm -rf -- "$temporary"
}
trap cleanup EXIT INT TERM

mkdir -p "$temporary/home/.ssh"
ssh-keygen -q -t ed25519 -N '' -f "$temporary/id_ed25519"
ssh-keygen -q -t ed25519 -N 'fixture-passphrase' -f "$temporary/id_ed25519_encrypted"
cat "$temporary/id_ed25519.pub" "$temporary/id_ed25519_encrypted.pub" > "$temporary/authorized_keys"

sh "$fixture" start "$temporary/authorized_keys"

known_hosts="$temporary/home/.ssh/known_hosts"
scan_host() {
    port=$1
    attempts=0
    until ssh-keyscan -T 2 -p "$port" 127.0.0.1 >> "$known_hosts" 2>/dev/null; do
        attempts=$((attempts + 1))
        [ "$attempts" -lt 20 ] || { echo "SSH fixture port $port did not become ready" >&2; exit 1; }
        sleep 1
    done
}
for port in 2222 2223 2224 2225 2226 2227; do
    scan_host "$port"
done
ssh-keyscan -T 2 -p 2223 127.0.0.1 2>/dev/null \
    | awk '{$1="pi-desk-fixture-via-bastion"; print}' >> "$known_hosts"
chmod 0600 "$known_hosts"

config="$temporary/home/.ssh/config"
cat > "$config" <<EOF
Host pi-desk-fixture
    HostName 127.0.0.1
    Port 2222
    User fixture
    IdentityFile $temporary/id_ed25519
    IdentitiesOnly yes
    IdentityAgent none
    UserKnownHostsFile $known_hosts
    StrictHostKeyChecking yes

Host pi-desk-fixture-admin
    HostName 127.0.0.1
    Port 2223
    User fixture
    IdentityFile $temporary/id_ed25519
    IdentitiesOnly yes
    IdentityAgent none
    UserKnownHostsFile $known_hosts
    StrictHostKeyChecking yes

Host pi-desk-fixture-bastion
    HostName 127.0.0.1
    Port 2224
    User fixture
    IdentityFile $temporary/id_ed25519
    IdentitiesOnly yes
    IdentityAgent none
    UserKnownHostsFile $known_hosts
    StrictHostKeyChecking yes

Host pi-desk-fixture-password
    HostName 127.0.0.1
    Port 2225
    User fixture
    IdentityFile $temporary/id_ed25519
    IdentitiesOnly yes
    IdentityAgent none
    UserKnownHostsFile $known_hosts
    StrictHostKeyChecking yes

Host pi-desk-fixture-encrypted
    HostName 127.0.0.1
    Port 2222
    User fixture
    IdentityFile $temporary/id_ed25519_encrypted
    IdentitiesOnly yes
    IdentityAgent none
    UserKnownHostsFile $known_hosts
    StrictHostKeyChecking yes

Host pi-desk-fixture-nohome
    HostName 127.0.0.1
    Port 2226
    User fixture
    IdentityFile $temporary/id_ed25519
    IdentitiesOnly yes
    IdentityAgent none
    UserKnownHostsFile $known_hosts
    StrictHostKeyChecking yes

Host pi-desk-fixture-readonly
    HostName 127.0.0.1
    Port 2227
    User fixture
    IdentityFile $temporary/id_ed25519
    IdentitiesOnly yes
    IdentityAgent none
    UserKnownHostsFile $known_hosts
    StrictHostKeyChecking yes

Host pi-desk-fixture-jump
    HostName pi-desk-ssh-fixture
    Port 22
    User fixture
    IdentityFile $temporary/id_ed25519
    IdentitiesOnly yes
    IdentityAgent none
    UserKnownHostsFile $known_hosts
    StrictHostKeyChecking yes
    HostKeyAlias pi-desk-fixture-via-bastion
    ProxyJump pi-desk-fixture-bastion
EOF
chmod 0600 "$config"

marker="$temporary/consent-marker"
consent_config="$temporary/consent-config"
cp "$config" "$consent_config"
cat >> "$consent_config" <<EOF

Match originalhost pi-desk-fixture exec "touch $marker"
EOF
chmod 0600 "$consent_config"

cd "$root"
go run ./cmd/pi-desk-remote-artifacts \
    -output build/remote-helper/artifacts \
    -build-identity "$build_identity" \
    -pi-min 0.84.2 \
    -pi-max 0.85.0

export HOME="$temporary/home"
export GOCACHE="$original_gocache"
export GOMODCACHE="$original_gomodcache"
export PI_DESK_SSH_LIVE_CONFIG="$config"
export PI_DESK_SSH_LIVE_CONSENT_CONFIG="$consent_config"
export PI_DESK_SSH_LIVE_CONSENT_MARKER="$marker"
export PI_DESK_SSH_LIVE_TARGET=pi-desk-fixture
export PI_DESK_SSH_LIVE_ADMIN_TARGET=pi-desk-fixture-admin
export PI_DESK_SSH_LIVE_TOXIPROXY_URL=http://127.0.0.1:8474
export PI_DESK_SSH_LIVE_PASSWORD_TARGET=pi-desk-fixture-password
export PI_DESK_SSH_LIVE_ENCRYPTED_TARGET=pi-desk-fixture-encrypted
export PI_DESK_SSH_LIVE_PROXYJUMP_TARGET=pi-desk-fixture-jump
export PI_DESK_SSH_LIVE_NOHOME_TARGET=pi-desk-fixture-nohome
export PI_DESK_SSH_LIVE_READONLY_TARGET=pi-desk-fixture-readonly
export PI_DESK_SSH_LIVE_DIRECTORY=/workspace/repo
export PI_DESK_SSH_LIVE_HELPER="$helper"
export PI_DESK_SSH_LIVE_HELPER_OS=linux
export PI_DESK_SSH_LIVE_HELPER_ARCH=amd64
export PI_DESK_SSH_LIVE_HELPER_BUILD="$build_identity"

go test ./internal/remotessh -run '^TestLiveSSH' -count=1 -v -timeout=300s

# The cross-package tests intentionally construct the production Locator, which
# does not accept a config path. OpenSSH resolves ~/.ssh from the passwd entry
# rather than $HOME, so route only these fixture processes through the real
# client with the isolated config already created above.
real_ssh=$(command -v ssh)
mkdir -p "$temporary/bin"
cat > "$temporary/bin/ssh" <<'EOF'
#!/bin/sh
exec "$PI_DESK_SSH_LIVE_REAL_SSH" -F "$PI_DESK_SSH_LIVE_CONFIG" "$@"
EOF
chmod 0700 "$temporary/bin/ssh"
export PI_DESK_SSH_LIVE_REAL_SSH="$real_ssh"
export PATH="$temporary/bin:$PATH"

go test ./internal/repository -run '^TestLiveRemoteRepositoryAndTerminalBackends$' -count=1 -v -timeout=180s

# Service-level verification probes and starts the real Pi runtime. Install the
# pinned compatible package into the disposable fixture instead of faking a Pi
# executable or touching the user's global npm installation.
npm install --prefix "$temporary/pi-runtime" --ignore-scripts --no-audit --no-fund \
    @earendil-works/pi-coding-agent@0.84.3
export PATH="$temporary/pi-runtime/node_modules/.bin:$PATH"

CGO_ENABLED=1 go test ./internal/appservice \
    -run '^(TestLiveRemoteWorkspaceServiceSetup|TestLiveRemoteWorkspaceLifecycle)$' \
    -count=1 -v -timeout=300s
