#!/bin/sh
# Builds the release files for a tag into dist/TAG: ghv and ghv-apply for
# each platform, and SHA256SUMS. Run it in a clean checkout at the tag. CI
# runs it on the same tag, so its SHA256SUMS must match yours.
set -eu

tag=${1:-}
if ! printf '%s\n' "$tag" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'; then
	echo "usage: scripts/release.sh vX.Y.Z" >&2
	exit 2
fi
cd "$(git rev-parse --show-toplevel)"
if [ "$(git rev-parse HEAD)" != "$(git rev-parse -q --verify "$tag^{commit}" || true)" ]; then
	echo "release: HEAD is not at $tag" >&2
	exit 1
fi
# Untracked files would mark the binaries +dirty.
if [ -n "$(git status --porcelain)" ]; then
	echo "release: the checkout has changes; commit or remove them first" >&2
	exit 1
fi

# go.mod's toolchain line is only a minimum; a newer local Go would build
# different bytes than CI.
GOTOOLCHAIN=$(sed -n 's/^toolchain //p' go.mod)
CGO_ENABLED=0
# The ghv* glob sorts by the locale's collation; macOS puts _ before -.
LC_ALL=C
export GOTOOLCHAIN CGO_ENABLED LC_ALL

out=dist/$tag
rm -rf "$out"
mkdir -p "$out"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
	for cmd in ghv ghv-apply; do
		GOOS=${target%/*} GOARCH=${target#*/} go build -trimpath -o "$out/${cmd}_${tag}_${target%/*}_${target#*/}" "./cmd/$cmd"
	done
done
cd "$out"
if command -v sha256sum >/dev/null; then
	sha256sum ghv* >SHA256SUMS
else
	shasum -a 256 ghv* >SHA256SUMS
fi
echo "Built $out:"
cat SHA256SUMS
