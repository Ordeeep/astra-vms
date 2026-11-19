#!/usr/bin/env bash
set -euo pipefail
[[ $EUID == 0 ]] || exit 1
source /etc/astra/backup.env
: "${ASTRA_BACKUP_ROOT:?Configure an external mounted backup destination}"
[[ $ASTRA_BACKUP_ROOT == /* && -d $ASTRA_BACKUP_ROOT ]] || exit 1
mount_target=$(findmnt -n -o TARGET -T "$ASTRA_BACKUP_ROOT")
[[ $mount_target != / && -n $mount_target ]] || { echo 'Daily backup requires a separately mounted destination.' >&2; exit 1; }
args=()
if [[ ${ASTRA_BACKUP_ARCHIVE:-0} == 1 ]]; then args+=(--include-archive); fi
exec /opt/astra/current/backup.sh "$ASTRA_BACKUP_ROOT/$(date -u +%Y%m%dT%H%M%SZ)" "${args[@]}"
