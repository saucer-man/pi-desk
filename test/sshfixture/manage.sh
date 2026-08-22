#!/bin/sh
set -eu

network=pi-desk-ssh-fixture
sshd=pi-desk-ssh-fixture
password=pi-desk-ssh-password
bastion=pi-desk-ssh-bastion
nohome=pi-desk-ssh-nohome
readonly=pi-desk-ssh-readonly
proxy=pi-desk-ssh-proxy
image=pi-desk-ssh-fixture:local
proxy_image=ghcr.io/shopify/toxiproxy:2.12.0@sha256:9378ed52a28bc50edc1350f936f518f31fa95f0d15917d6eb40b8e376d1a214e
api=http://127.0.0.1:8474

usage() {
    echo "usage: $0 start <authorized-key.pub> | down | up | status | stop" >&2
    exit 2
}

set_proxy_enabled() {
    curl -fsS -X POST "$api/proxies/ssh" \
        -H 'Content-Type: application/json' \
        -d "{\"enabled\":$1}" >/dev/null
}

case "${1:-}" in
start)
    [ "$#" -eq 2 ] || usage
    public_key=$2
    [ -s "$public_key" ] || { echo "public key not found: $public_key" >&2; exit 1; }
    root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

    docker network inspect "$network" >/dev/null 2>&1 || docker network create "$network" >/dev/null
    docker build -t "$image" "$root"
    docker rm -f "$proxy" "$sshd" "$password" "$bastion" "$nohome" "$readonly" >/dev/null 2>&1 || true
    docker run -d --name "$sshd" --network "$network" \
        -p 127.0.0.1:2223:22 \
        -v "$public_key:/keys/authorized_keys:ro" \
        -v pi-desk-ssh-home:/home/fixture \
        -v pi-desk-ssh-hostkeys:/hostkeys \
        -v pi-desk-ssh-workspace:/workspace \
        "$image" >/dev/null
    docker run -d --name "$password" --network "$network" \
        -p 127.0.0.1:2225:22 \
        -e PI_DESK_SSH_FIXTURE_MODE=password \
        -v "$public_key:/keys/authorized_keys:ro" \
        -v pi-desk-ssh-password-home:/home/fixture \
        -v pi-desk-ssh-password-hostkeys:/hostkeys \
        -v pi-desk-ssh-password-workspace:/workspace \
        "$image" >/dev/null
    docker run -d --name "$bastion" --network "$network" \
        -p 127.0.0.1:2224:22 \
        -e PI_DESK_SSH_FIXTURE_MODE=bastion \
        -v "$public_key:/keys/authorized_keys:ro" \
        -v pi-desk-ssh-bastion-home:/home/fixture \
        -v pi-desk-ssh-bastion-hostkeys:/hostkeys \
        -v pi-desk-ssh-bastion-workspace:/workspace \
        "$image" >/dev/null
    docker run -d --name "$nohome" --network "$network" \
        -p 127.0.0.1:2226:22 \
        -e PI_DESK_SSH_FIXTURE_MODE=nohome \
        -v "$public_key:/keys/authorized_keys:ro" \
        -v pi-desk-ssh-nohome-home:/home/fixture \
        -v pi-desk-ssh-nohome-hostkeys:/hostkeys \
        -v pi-desk-ssh-nohome-workspace:/workspace \
        "$image" >/dev/null
    docker run -d --name "$readonly" --network "$network" \
        -p 127.0.0.1:2227:22 \
        -e PI_DESK_SSH_FIXTURE_MODE=readonly \
        -v "$public_key:/keys/authorized_keys:ro" \
        -v pi-desk-ssh-readonly-home:/home/fixture \
        -v pi-desk-ssh-readonly-hostkeys:/hostkeys \
        -v pi-desk-ssh-readonly-workspace:/workspace \
        "$image" >/dev/null
    docker run -d --name "$proxy" --network "$network" \
        -p 127.0.0.1:2222:2222 \
        -p 127.0.0.1:8474:8474 \
        "$proxy_image" >/dev/null

    attempts=0
    until curl -fsS "$api/version" >/dev/null 2>&1; do
        attempts=$((attempts + 1))
        [ "$attempts" -lt 30 ] || { docker logs "$proxy" >&2; exit 1; }
        sleep 1
    done
    curl -fsS -X POST "$api/proxies" \
        -H 'Content-Type: application/json' \
        -d '{"name":"ssh","listen":"0.0.0.0:2222","upstream":"pi-desk-ssh-fixture:22","enabled":true}' >/dev/null
    echo "fixture ready: proxy 2222, admin 2223, bastion 2224, password 2225, no-home 2226, read-only 2227"
    ;;
down)
    [ "$#" -eq 1 ] || usage
    set_proxy_enabled false
    echo "SSH proxy disabled"
    ;;
up)
    [ "$#" -eq 1 ] || usage
    set_proxy_enabled true
    echo "SSH proxy enabled"
    ;;
status)
    [ "$#" -eq 1 ] || usage
    curl -fsS "$api/proxies/ssh"
    ;;
stop)
    [ "$#" -eq 1 ] || usage
    docker rm -f "$proxy" "$sshd" "$password" "$bastion" "$nohome" "$readonly" >/dev/null 2>&1 || true
    echo "fixture containers stopped; named volumes were retained"
    ;;
*)
    usage
    ;;
esac
