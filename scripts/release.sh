#!/bin/sh
# Builds what a release of sereno consists of (requirement R41) into a folder:
# six archives, checksums.txt, install.ps1 and install.sh.
#
#   sh scripts/release.sh VERSION [OUT]
#
# VERSION is the tag of the release, for example v0.1.0. It is refused unless
# it is a version the go command accepts for this module (vMAJOR.MINOR.PATCH,
# optionally with a pre-release part such as -rc.1). OUT is the folder to
# write into; the default is dist, which .gitignore lists.
#
# In order:
#   1. the six programs, each with
#        CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=VERSION"
#      for windows, darwin and linux on amd64 and arm64, into a temporary
#      folder that is removed at the end;
#   2. `go run ./internal/tools/release`, which packs each program with LICENSE
#      and README.md, writes checksums.txt and renders the two install scripts
#      from scripts/install.ps1.tmpl and scripts/install.sh.tmpl.
#
# The programs are built and packed, never started. Nothing is uploaded,
# tagged or published here: .github/workflows/release.yml does that with the
# folder this script leaves, and the CI workflow runs the same script to see
# that a release could be built.
#
# GITHUB_REPOSITORY (OWNER/NAME) is the repository the install scripts download
# from; without it, the one of this project. SOURCE_DATE_EPOCH is the time the
# archive entries carry; without it, the time of the last commit.

set -eu
cd "$(dirname "$0")/.."

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
    printf 'usage: sh scripts/release.sh VERSION [OUT]\n' >&2
    exit 2
fi
version=$1
out=${2:-dist}

go run ./internal/tools/release --version "$version" --check

work=$(mktemp -d "${TMPDIR:-/tmp}/sereno-release-XXXXXX")
trap 'rm -rf "$work"' EXIT

go version
for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
    os=${target%/*}
    arch=${target#*/}
    name=sereno
    if [ "$os" = windows ]; then name=sereno.exe; fi
    printf 'building %s/%s\n' "$os" "$arch"
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$work/${os}_$arch/$name" ./cmd/sereno
done

epoch=${SOURCE_DATE_EPOCH-}
if [ -z "$epoch" ]; then epoch=$(git log -1 --format=%ct 2> /dev/null || :); fi
if [ -n "$epoch" ]; then
    go run ./internal/tools/release --version "$version" --binaries "$work" --out "$out" --repository "${GITHUB_REPOSITORY:-joaooliveira10/serena-terminal}" --epoch "$epoch"
else
    go run ./internal/tools/release --version "$version" --binaries "$work" --out "$out" --repository "${GITHUB_REPOSITORY:-joaooliveira10/serena-terminal}"
fi
