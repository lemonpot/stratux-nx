#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cp "$root/overlay/main/flightlog.go" "$root/overlay/main/flightlog_journal.go" "$root/overlay/main/flightlog_weather.go" "$root/overlay/main/flightlog_journal_test.go" "$work/"
cp "$root/tests/flightlog-stub.go.txt" "$work/stub.go"
cd "$work"
go mod init stratux-nx-flightlog-test
go get github.com/ricochet2200/go-disk-usage/du@v0.0.0-20150921141558-f0d1b743428f
go test -race -v ./...
