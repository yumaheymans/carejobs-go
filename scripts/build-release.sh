#!/usr/bin/env bash
# Builds the release archives of the carejobs command into dist/:
#
#   carejobs_<version>_<os>_<arch>.zip      for Windows (carejobs.exe, LICENSE, README.md)
#   carejobs_<version>_<os>_<arch>.tar.gz   for macOS and Linux (carejobs, LICENSE, README.md)
#   checksums.txt                           SHA-256 of every archive
#
# Usage: scripts/build-release.sh [VERSION]
#
# The version is the Version constant of client.go. Pass VERSION (for example the
# tag without its "v") to make the script stop when the two disagree. The release
# workflow does that, so a tag can never ship a binary that prints another version.
#
# Needs Go, python3 and bash. The binaries are static (CGO_ENABLED=0) and built
# with -trimpath and no VCS stamp, and the archives carry fixed timestamps and
# owners, so the same toolchain turns the same source into the same bytes.
set -euo pipefail
cd "$(dirname "$0")/.."

version="$(sed -n 's/^[[:space:]]*Version = "\(.*\)"$/\1/p' client.go)"
if [ -z "$version" ]; then
  echo "build-release: cannot read Version from client.go" >&2
  exit 1
fi
if [ $# -ge 1 ] && [ "$1" != "$version" ]; then
  echo "build-release: asked for $1 but client.go says Version = \"$version\"" >&2
  exit 1
fi

targets="windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64"

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
rm -rf dist
mkdir -p dist

for target in $targets; do
  os="${target%/*}"
  arch="${target#*/}"
  exe=carejobs
  if [ "$os" = windows ]; then exe=carejobs.exe; fi
  mkdir -p "$stage/${os}_${arch}"
  echo "building $os/$arch"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -buildvcs=false -ldflags "-s -w" -o "$stage/${os}_${arch}/$exe" ./cmd/carejobs
done

python3 -I - "$stage" "$version" dist $targets <<'PY'
import calendar
import gzip
import hashlib
import io
import sys
import tarfile
import zipfile

stage, version, dist, *targets = sys.argv[1:]
stamp = (2026, 10, 19, 0, 0, 0)  # fixed, so archives do not depend on when they were built
epoch = calendar.timegm(stamp)

sums = []
for target in targets:
    goos, goarch = target.split("/")
    exe = "carejobs.exe" if goos == "windows" else "carejobs"
    members = [
        (exe, f"{stage}/{goos}_{goarch}/{exe}", 0o755),
        ("LICENSE", "LICENSE", 0o644),
        ("README.md", "README.md", 0o644),
    ]
    base = f"carejobs_{version}_{goos}_{goarch}"
    if goos == "windows":
        name = f"{base}.zip"
        with zipfile.ZipFile(f"{dist}/{name}", "w", zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
            for member, source, mode in members:
                with open(source, "rb") as handle:
                    data = handle.read()
                info = zipfile.ZipInfo(member, date_time=stamp)
                info.create_system = 3
                info.external_attr = (0o100000 | mode) << 16
                info.compress_type = zipfile.ZIP_DEFLATED
                archive.writestr(info, data, compresslevel=9)
    else:
        name = f"{base}.tar.gz"
        with open(f"{dist}/{name}", "wb") as raw:
            with gzip.GzipFile(filename="", mode="wb", compresslevel=9, fileobj=raw, mtime=0) as packed:
                with tarfile.open(fileobj=packed, mode="w", format=tarfile.GNU_FORMAT) as archive:
                    for member, source, mode in members:
                        with open(source, "rb") as handle:
                            data = handle.read()
                        info = tarfile.TarInfo(member)
                        info.size = len(data)
                        info.mode = mode
                        info.mtime = epoch
                        info.uid = info.gid = 0
                        info.uname = info.gname = ""
                        archive.addfile(info, io.BytesIO(data))
    with open(f"{dist}/{name}", "rb") as handle:
        sums.append((name, hashlib.sha256(handle.read()).hexdigest()))

with open(f"{dist}/checksums.txt", "w", newline="\n") as out:
    for name, digest in sorted(sums):
        out.write(f"{digest}  {name}\n")
PY

echo
cat dist/checksums.txt
