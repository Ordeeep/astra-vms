#!/usr/bin/env bash
set -euo pipefail
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || { echo 'Build requires Linux x86_64.' >&2; exit 1; }
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
version=${ASTRA_VERSION:-$(date -u +%Y%m%dT%H%M%SZ)}
[[ $version =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$ ]] || exit 1
: "${MEDIAMTX:?Set MEDIAMTX to the Linux executable path}"
for tool in go node npm ffmpeg ffprobe python3; do command -v "$tool" >/dev/null; done
[[ $(node -p 'process.versions.node.split(".")[0]') == 24 ]] || { echo 'Node.js 24 required.' >&2; exit 1; }
[[ -x $MEDIAMTX ]] || exit 1
dest="$root/backend/dist/astra-linux-amd64-$version"
[[ ! -e $dest ]] || { echo 'Package version already exists.' >&2; exit 1; }
mkdir -p "$dest/bin" "$dest/frontend" "$dest/systemd" "$dest/licenses"
(cd "$root/backend"; go build -trimpath -o "$dest/bin/vms" ./cmd/vms; go build -trimpath -o "$dest/bin/vms-worker" ./cmd/vms-worker)
(cd "$root/backend"; go build -trimpath -o "$dest/bin/vms-event-worker" ./cmd/vms-event-worker)
(cd "$root/frontend"; npm ci --no-audit --no-fund; NEXT_TELEMETRY_DISABLED=1 npm run build)
cp -a "$root/frontend/.next/standalone/." "$dest/frontend/"
install -m 0755 "$(command -v node)" "$dest/bin/node"
install -m 0755 "$MEDIAMTX" "$dest/bin/mediamtx"
node_license="$(dirname -- "$(dirname -- "$(readlink -f "$(command -v node)")")")/LICENSE"
cp "$node_license" "$dest/licenses/node-LICENSE.txt"
cp "$(dirname -- "$MEDIAMTX")/LICENSE" "$dest/licenses/mediamtx-LICENSE.txt"
cp "$root/backend/deploy/linux/"*.sh "$dest/"
cp "$root/backend/deploy/linux/package-readme.md" "$dest/README.md"
chmod 0755 "$dest/"*.sh
cp "$root/backend/deploy/linux/systemd/"*.service "$root/backend/deploy/linux/systemd/"*.timer "$dest/systemd/"
cp "$root/LICENSE" "$dest/licenses/astra-MIT.txt"
cp "$root/backend/deploy/windows/THIRD-PARTY.md" "$dest/licenses/THIRD-PARTY.md"
printf '%s\n' "$version" > "$dest/VERSION"
{
  go version; node --version; "$MEDIAMTX" --version
  ffmpeg -version | head -n 1
  printf 'source=%s\n' "${ASTRA_SOURCE_REVISION:-$(git -C "$root" rev-parse HEAD 2>/dev/null || echo source-snapshot)}"
} > "$dest/BUILD-INFO"
(cd "$dest"; find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
printf 'Package: %s\n' "$dest"
