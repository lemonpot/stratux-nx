#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cp "$root/overlay/main/ahrs_v2.go" "$root/overlay/main/ahrs_v2_test.go" "$work/"
cp "$root/tests/ahrs-settings-stub.go.txt" "$work/settings_stub.go"
cd "$work"
go mod init stratux-nx-ahrs-test
go get github.com/stratux/goflying/ahrs@dd059ec481946361d63dd9c591e2a0efbe4add0a
go test -race -v ./...
