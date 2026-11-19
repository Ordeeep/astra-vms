#!/usr/bin/env bash
set -euo pipefail
umask 077
[[ $EUID == 0 ]] || exit 1
destination=${1:?Usage: backup.sh NEW_ABSOLUTE_DIRECTORY [--include-archive]}
[[ $destination == /* && ! -e $destination && ! -L $destination ]] || { echo 'Use a new absolute backup directory.' >&2; exit 1; }
destination=$(realpath -m -- "$destination")
[[ ! -e $destination && ! -L $destination ]] || exit 1
case "$destination/" in /srv/astra/*|/var/lib/astra/*|/etc/astra/*|/opt/astra/*) echo 'Backup must be outside application and archive directories.' >&2; exit 1;; esac
include_archive=false
if [[ ${2:-} == --include-archive ]]; then include_archive=true; elif [[ $# != 1 ]]; then exit 1; fi
[[ -f /etc/astra/installation ]] || exit 1
restart=()
for service in astra-core astra-media; do if systemctl is-active --quiet "$service"; then restart+=("$service"); fi; done
restore_services() { if ((${#restart[@]})); then systemctl start "${restart[@]}"; fi; }
trap restore_services EXIT
systemctl stop astra-core.service astra-media.service
mkdir -p -m 0700 -- "$(dirname -- "$destination")"
mkdir -m 0700 -- "$destination"
runuser -u postgres -- pg_dump --format=custom astra_vms > "$destination/database.dump"
tar -C / -czf "$destination/private-state.tar.gz" etc/astra var/lib/astra
readlink -f /opt/astra/current > "$destination/release.txt"
if $include_archive; then tar -C /srv/astra -czf "$destination/archive.tar.gz" archive; fi
(cd "$destination"; find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
printf 'Backup staged: %s. Copy to protected storage outside this VM.\n' "$destination"
