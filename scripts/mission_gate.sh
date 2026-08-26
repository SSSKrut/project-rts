#!/usr/bin/env bash
# Block D gate: every mission twice, headless. The verdict line must be the
# same both times AND the hash logs byte-identical — a mission is a scenario,
# and a scenario that does not reproduce cannot be balanced.
set -u
cd "$(dirname "$0")/.."

MISSIONS=${@:-"test_hold test_timeout crossroads"}
D=/tmp/rts-mission

go build -o bin/rts . || exit 1
mkdir -p "$D"
fail=0
for m in $MISSIONS; do
  v1=$(timeout 300 ./bin/rts -mission="$m" -replay-hash="$D/${m}_1.txt" 2>/dev/null | grep -o "MISSION.*")
  v2=$(timeout 300 ./bin/rts -mission="$m" -replay-hash="$D/${m}_2.txt" 2>/dev/null | grep -o "MISSION.*")
  if diff -q "$D/${m}_1.txt" "$D/${m}_2.txt" >/dev/null 2>&1; then
    hash_ok="HASH OK"
  else
    hash_ok="HASH MISMATCH"
    fail=1
  fi
  printf "%-16s %-14s %s\n" "$m" "$hash_ok" "$v1"
  [ "$v1" != "$v2" ] && { printf "%-16s VERDICT DIVERGED: %s\n" "$m" "$v2"; fail=1; }
  [ -z "$v1" ] && { printf "%-16s NO VERDICT\n" "$m"; fail=1; }
done
exit $fail
