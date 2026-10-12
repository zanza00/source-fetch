#!/usr/bin/env bash
# End-to-end smoke test. Uses a local bare repo only: no network.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

pass() { printf 'ok   %s\n' "$1"; }
fail() { printf 'FAIL %s\n' "$1"; exit 1; }

bin="$tmp/source-fetch"
(cd "$root" && go build -o "$bin" ./cmd/source-fetch)

# --- fixture: a local bare repo with one commit tagged v1.2.3 --------------
work="$tmp/work"
git init -q "$work"
git -C "$work" config user.email t@example.com
git -C "$work" config user.name t
echo hi >"$work/file.txt"
git -C "$work" add .
git -C "$work" commit -q -m init
git -C "$work" tag v1.2.3
bare="$tmp/bare.git"
git clone -q --bare "$work" "$bare"

# --- consuming project -----------------------------------------------------
proj="$tmp/proj"
mkdir -p "$proj"
cat >"$proj/.source-fetch.toml" <<EOF
[source.tool]
url = "$bare"
version_cmd = "echo 1.2.3"
tag_prefix = "v"
EOF

export SOURCE_FETCH_CACHE="$tmp/cache"
cd "$proj"

# manifest discovery
"$bin" manifest | grep -q ".source-fetch.toml" || fail "manifest discovery"
pass "manifest discovery"

# path: fetch + print dir, pinned to the version tag
out="$("$bin" path tool)"
[ -d "$out/.git" ] || fail "path did not produce a checkout"
[ "$(head -n1 "$out/.source-fetch-ref")" = "ref=v1.2.3" ] || fail "marker ref"
pass "path pins to version tag"

# second call is a fast path (offline)
out2="$("$bin" path tool)"
[ "$out" = "$out2" ] || fail "path changed between calls"
pass "offline fast path"

# explicit one-shot ref
sha="$(git -C "$out" rev-parse HEAD)"
out3="$("$bin" tool "$sha")"
[ "$out3" = "$out" ] || fail "explicit ref reused cache"
pass "explicit ref"

# list does not fail
"$bin" list >/dev/null || fail "list"
pass "list"

# --- one-off peek (ad-hoc cache) --------------------------------------------
adhoc_url="file://$bare"
out_p="$("$bin" peek "$adhoc_url")"
[ -d "$out_p/.git" ] || fail "peek did not produce a checkout"
[ -f "$out_p/.source-fetch-ref" ] || fail "peek marker"
pass "peek fetches a one-off URL"

# peek needs no manifest: fast path with discovery forced to fail
(cd "$tmp" && SOURCE_FETCH_MANIFEST=/nonexistent "$bin" peek "$adhoc_url") \
  >/dev/null 2>&1 || fail "peek needs no manifest"
pass "peek works without a manifest"

out_p2="$("$bin" peek "$adhoc_url")"
[ "$out_p" = "$out_p2" ] || fail "peek fast path"
pass "peek offline fast path"

out_p3="$("$bin" peek "$adhoc_url" v1.2.3)"
[ "$out_p3" = "$out_p" ] || fail "peek explicit ref should reuse the cache"
pass "peek with explicit ref"

"$bin" peek | grep -q "$bare" || fail "peek list"
pass "peek list"

"$bin" rm "$adhoc_url" >/dev/null 2>&1
[ ! -d "$out_p" ] || fail "peek rm"
pass "peek rm"

# --- rm + clean -------------------------------------------------------------
"$bin" rm tool >/dev/null 2>&1
[ ! -d "$out" ] || fail "rm did not delete"
pass "rm"

"$bin" path tool >/dev/null
"$bin" clean >/dev/null
[ ! -d "$out" ] || fail "clean did not delete"
pass "clean"

printf 'all smoke tests passed\n'
