# Local SSH fault fixture

This fixture runs only on loopback and keeps its key, known_hosts, host keys,
helper cache, and workspace separate from real SSH targets.

## Prerequisites

- WSL2 Ubuntu 24.04 with a running Docker daemon.
- Docker access through root (`wsl.exe -d Ubuntu-24.04 -u root -- ...`) or a
  user in the WSL `docker` group.
- Windows OpenSSH.
- A foreground WSL session while the fixture is running. With native Docker,
  WSL may stop the distribution and all containers when the last Windows-side
  `wsl.exe` process exits.

Do not reuse a production SSH key. Generate dedicated unencrypted and
encrypted fixture keys outside the repository, for example under
`%LOCALAPPDATA%\pi-desk-ssh-fixture\home\.ssh`. Pass an `authorized_keys`
file containing both public keys to `manage.sh start`. The encrypted key must
not be loaded into an agent while testing the batch-mode rejection path.

## Topology

- `127.0.0.1:2222`: SSH through Toxiproxy.
- `127.0.0.1:2223`: direct administrative SSH used only to observe markers.
- `127.0.0.1:2224`: key-only bastion with TCP forwarding enabled.
- `127.0.0.1:2225`: password-only target (`fixture` / `pi-desk-fixture`).
- `127.0.0.1:2226`: target with a non-writable home and no helper cache.
- `127.0.0.1:2227`: target with a read-only workspace root.
- `127.0.0.1:8474`: Toxiproxy control API.
- `/workspace/repo`: writable remote workspace.

The containers use separate named volumes for their fixture homes, host keys,
and workspaces. `stop` retains them so exact helper-cache reuse and stable
host identity can be tested.

## Start and control

From Windows Git Bash, keep a WSL process open for the test duration, for
example in another terminal with `wsl.exe -d Ubuntu-24.04 -- sleep 600`, then
run:

```sh
MSYS2_ARG_CONV_EXCL='*' wsl.exe -d Ubuntu-24.04 -u root -- \
  sh /mnt/<drive>/<repo>/test/sshfixture/manage.sh start \
  /mnt/c/Users/<windows-user>/AppData/Local/pi-desk-ssh-fixture/home/.ssh/authorized_keys

MSYS2_ARG_CONV_EXCL='*' wsl.exe -d Ubuntu-24.04 -u root -- \
  sh /mnt/<drive>/<repo>/test/sshfixture/manage.sh down

MSYS2_ARG_CONV_EXCL='*' wsl.exe -d Ubuntu-24.04 -u root -- \
  sh /mnt/<drive>/<repo>/test/sshfixture/manage.sh up
```

`manage.sh stop` removes the containers but retains named volumes. Remove the
three `pi-desk-ssh-*` volumes explicitly only when resetting host identity,
cache, and workspace is intended.

## Live tests

Create an isolated OpenSSH config containing:

- `pi-desk-fixture` on port 2222.
- `pi-desk-fixture-admin` on port 2223.
- `pi-desk-fixture-bastion` on port 2224.
- `pi-desk-fixture-password` on port 2225.
- `pi-desk-fixture-nohome` on port 2226.
- `pi-desk-fixture-readonly` on port 2227.
- `pi-desk-fixture-encrypted` on port 2222 using only the encrypted key.
- `pi-desk-fixture-jump` targeting Docker DNS name `pi-desk-ssh-fixture`
  through `pi-desk-fixture-bastion`, with a fixed `HostKeyAlias`.

All aliases must use the isolated known_hosts file. Then run:

```sh
MSYS2_ENV_CONV_EXCL='PI_DESK_SSH_LIVE_DIRECTORY;PI_DESK_SSH_LIVE_CONFIG' \
PI_DESK_SSH_LIVE_CONFIG='C:/Users/<windows-user>/AppData/Local/pi-desk-ssh-fixture/home/.ssh/config' \
PI_DESK_SSH_LIVE_CONSENT_CONFIG='C:/path/to/pi-desk-ssh-fixture/consent-config' \
PI_DESK_SSH_LIVE_CONSENT_MARKER='C:/path/to/pi-desk-ssh-fixture/consent-marker' \
PI_DESK_SSH_LIVE_TARGET='pi-desk-fixture' \
PI_DESK_SSH_LIVE_ADMIN_TARGET='pi-desk-fixture-admin' \
PI_DESK_SSH_LIVE_TOXIPROXY_URL='http://127.0.0.1:8474' \
PI_DESK_SSH_LIVE_PASSWORD_TARGET='pi-desk-fixture-password' \
PI_DESK_SSH_LIVE_ENCRYPTED_TARGET='pi-desk-fixture-encrypted' \
PI_DESK_SSH_LIVE_PROXYJUMP_TARGET='pi-desk-fixture-jump' \
PI_DESK_SSH_LIVE_NOHOME_TARGET='pi-desk-fixture-nohome' \
PI_DESK_SSH_LIVE_READONLY_TARGET='pi-desk-fixture-readonly' \
PI_DESK_SSH_LIVE_DIRECTORY='/workspace/repo' \
PI_DESK_SSH_LIVE_HELPER='C:/path/to/pi-desk/build/remote-helper/artifacts/helper-linux-amd64' \
PI_DESK_SSH_LIVE_HELPER_OS='linux' \
PI_DESK_SSH_LIVE_HELPER_ARCH='amd64' \
PI_DESK_SSH_LIVE_HELPER_BUILD='pi-desk-v1.0.1-p1' \
go test ./internal/remotessh \
  -run '^(TestLiveSSHConfigExecutionConsentBoundary|TestLiveSSHConnectionFixture|TestLiveSSHAuthenticationAndProxyJumpMatrix|TestLiveSSHRestrictedFilesystemMatrix|TestLiveSSHConcurrentHelperInstallers|TestLiveSSHHelperBootstrap|TestLiveSSHNetworkDisconnectFaults)$' \
  -count=1 -v -timeout=240s
```

`PI_DESK_SSH_LIVE_CONFIG` is consumed only by live tests. Production code and
Wails APIs do not accept an executable, config path, SSH options, or command.
The consent config must add a `Match originalhost pi-desk-fixture exec
"<marker-command>"` rule to the otherwise isolated fixture config. The consent
test first performs static discovery and proves the marker is absent, then
runs the explicitly consented preflight and requires the marker to exist.
The service-level no-consent path is separately checked before lifecycle
access.

The restricted-filesystem test requires helper installation to fail and revoke
the generation when home is not writable, while a read-only workspace remains
readable and returns a stable write failure without creating a file. The
concurrent-installer test uses a one-run artifact hash and races two independent SSH
connections publishing the same immutable artifact.

The fault test covers accepted Bash, a committed conditional write whose
response is lost, an active Terminal stream disconnect, and an out-of-band
helper crash. Each operation must revoke its lease/generation and require an
explicit higher-generation reconnect; already-dispatched mutations/processes
must project outcome-unknown. The final pre-dispatch case revokes the task
lease before calling conditional write and requires generation-revoked with
no remote file. The test restores the proxy, removes
its latency toxic, and cleans markers even when an assertion fails.
