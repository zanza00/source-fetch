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

# manifest add/set/rm edit the manifest file
"$bin" manifest add tool2 "$bare" --version-cmd "echo 1.2.3" --tag-prefix v >/dev/null 2>&1 \
  || fail "manifest add"
grep -q '\[source.tool2\]' "$proj/.source-fetch.toml" || fail "manifest add not written"
"$bin" manifest set tool2 ref=v1.2.3 >/dev/null 2>&1 || fail "manifest set"
grep -q 'ref = "v1.2.3"' "$proj/.source-fetch.toml" || fail "manifest set not written"
"$bin" manifest rm tool2 >/dev/null 2>&1 || fail "manifest rm"
if grep -q '\[source.tool2\]' "$proj/.source-fetch.toml"; then
  fail "manifest rm did not remove the entry"
fi
pass "manifest add/set/rm"

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

# --- history depth: six commits, a tag on the third -------------------------
hwork="$tmp/hwork"
git init -q "$hwork"
git -C "$hwork" symbolic-ref HEAD refs/heads/main
git -C "$hwork" config user.email t@example.com
git -C "$hwork" config user.name t
for i in 1 2 3 4 5 6; do
  echo "$i" >"$hwork/file$i.txt"
  git -C "$hwork" add .
  git -C "$hwork" commit -q -m "commit $i"
  if [ "$i" = 3 ]; then git -C "$hwork" tag v1.0.0; fi
done
hbare="$tmp/hbare.git"
git clone -q --bare "$hwork" "$hbare"
hurl="file://$hbare"

"$bin" manifest add hist "$hurl" >/dev/null 2>&1 || fail "manifest add hist"

hist_out="$("$bin" path hist --depth 3)"
[ "$(git -C "$hist_out" rev-list --count HEAD)" = 3 ] || fail "path --depth 3"
[ "$(git -C "$hist_out" rev-parse --is-shallow-repository)" = true ] || fail "path --depth 3 not shallow"
pass "path --depth 3"

# A shallower request is a no-op: history is never reduced.
"$bin" path hist --depth 2 >/dev/null || fail "path --depth 2 no-op"
[ "$(git -C "$hist_out" rev-list --count HEAD)" = 3 ] || fail "--depth reduced history"
pass "--depth does not reduce history"

"$bin" update hist --deep >/dev/null || fail "update --deep"
[ "$(git -C "$hist_out" rev-list --count HEAD)" = 6 ] || fail "update --deep did not fetch full history"
[ "$(git -C "$hist_out" rev-parse --is-shallow-repository)" = false ] || fail "update --deep still shallow"
pass "update --deep"

# update without flags stays shallow.
"$bin" update hist >/dev/null || fail "update default"
[ "$(git -C "$hist_out" rev-list --count HEAD)" = 1 ] || fail "update default not shallow"
pass "update stays shallow"

# A leading depth flag routes to the default fetch command.
"$bin" --deep hist >/dev/null || fail "--deep default fetch"
[ "$(git -C "$hist_out" rev-list --count HEAD)" = 6 ] || fail "leading --deep did not fetch full history"
pass "leading --deep"

if "$bin" path hist --depth 0 >/dev/null 2>&1; then fail "--depth 0 accepted"; fi
if "$bin" path hist --deep --depth 2 >/dev/null 2>&1; then fail "--deep with --depth accepted"; fi
pass "depth flag validation"

# --- deep link through insteadOf (offline) ----------------------------------
gitconfig="$tmp/gitconfig"
cat >"$gitconfig" <<EOF
[url "$hurl"]
	insteadOf = https://github.com/acme/tool
EOF
link_out="$(GIT_CONFIG_GLOBAL="$gitconfig" GIT_CONFIG_NOSYSTEM=1 \
  "$bin" peek https://github.com/acme/tool/blob/main/file1.txt --depth 3)"
[ -f "$link_out" ] || fail "deep link did not print an existing file"
link_dir="$(dirname "$link_out")"
[ "$(git -C "$link_dir" rev-list --count HEAD)" = 3 ] || fail "deep link ignored --depth 3"
GIT_CONFIG_GLOBAL="$gitconfig" GIT_CONFIG_NOSYSTEM=1 \
  "$bin" rm https://github.com/acme/tool >/dev/null 2>&1 || fail "rm deep link"
pass "deep link with --depth"

# --- rm + clean -------------------------------------------------------------
"$bin" rm tool >/dev/null 2>&1
[ ! -d "$out" ] || fail "rm did not delete"
pass "rm"

"$bin" path tool >/dev/null
"$bin" clean >/dev/null
[ ! -d "$out" ] || fail "clean did not delete"
pass "clean"

printf 'all smoke tests passed\n'
