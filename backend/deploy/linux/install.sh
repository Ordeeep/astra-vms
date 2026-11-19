#!/usr/bin/env bash
set -euo pipefail
umask 077
[[ $EUID == 0 ]] || { echo 'Run as root.' >&2; exit 1; }
source /etc/os-release
[[ $ID == ubuntu && $VERSION_ID == 24.04 && $(uname -m) == x86_64 ]] || { echo 'Ubuntu 24.04 x86_64 required.' >&2; exit 1; }
bundle=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
public_host=${1:?Usage: install.sh PUBLIC_IP_OR_DNS}
[[ $public_host =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]*$ ]] || { echo 'Invalid host.' >&2; exit 1; }
for tool in psql pg_dump openssl runuser python3 curl ffmpeg ffprobe; do command -v "$tool" >/dev/null; done
[[ $(runuser -u postgres -- psql -Atqc 'SHOW server_version_num') == 16???? ]] || { echo 'PostgreSQL 16 required.' >&2; exit 1; }
pg_isready -q -h 127.0.0.1 -p 5432
(cd "$bundle"; sha256sum --check --quiet SHA256SUMS)
version=$(cat "$bundle/VERSION")
[[ $version =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$ ]] || exit 1
release="/opt/astra/releases/$version"
[[ ! -e $release && ! -L $release ]] || { echo 'Release already exists; use another version.' >&2; exit 1; }
fresh=true
if [[ -f /etc/astra/installation ]]; then
  [[ $(cat /etc/astra/installation) == astra-linux-v1 ]] || exit 1
  fresh=false
  [[ -f /etc/astra/core.env && -f /etc/astra/media.env && -L /opt/astra/current ]] || exit 1
  [[ -f /var/lib/astra/core/camera-key.bin ]] || { echo 'Existing encryption key missing; restore before upgrading.' >&2; exit 1; }
else
  for path in /etc/astra /var/lib/astra /opt/astra /srv/astra/archive; do [[ ! -e $path && ! -L $path ]] || { echo "Existing unowned path: $path" >&2; exit 1; }; done
  [[ $(runuser -u postgres -- psql -Atqc "SELECT count(*) FROM pg_roles WHERE rolname='astra_core'") == 0 ]] || exit 1
  [[ $(runuser -u postgres -- psql -Atqc "SELECT count(*) FROM pg_database WHERE datname='astra_vms'") == 0 ]] || exit 1
  for service in astra-core astra-media astra-web; do
    [[ ! -e /etc/systemd/system/$service.service ]] || { echo 'Service name already in use.' >&2; exit 1; }
    ! getent passwd "$service" >/dev/null || { echo 'Service account already in use.' >&2; exit 1; }
  done
fi
[[ $(findmnt -n -o FSTYPE -T /srv) == ext4 ]] || { echo '/srv must use local ext4.' >&2; exit 1; }
[[ $(stat -f -c %S /srv) == 4096 ]] || { echo 'ext4 must use 4096-byte blocks.' >&2; exit 1; }
if $fresh; then
  for account in astra-core astra-media astra-web; do useradd --system --user-group --no-create-home --shell /usr/sbin/nologin "$account"; done
  install -d -m 0755 /opt/astra /opt/astra/releases /var/lib/astra /srv/astra
  install -d -m 0750 -o root -g root /etc/astra
  install -d -m 0700 -o astra-core -g astra-core /var/lib/astra/core
  install -d -m 0700 -o astra-media -g astra-media /var/lib/astra/media /srv/astra/archive
  db_password=$(openssl rand -hex 32)
  worker_secret=$(openssl rand -hex 48)
  # The generated values are hex only. SQL stays on stdin, outside argv/logs.
  runuser -u postgres -- psql -v ON_ERROR_STOP=1 -q <<SQL
CREATE ROLE astra_core LOGIN PASSWORD '$db_password';
CREATE DATABASE astra_vms OWNER astra_core;
SQL
  printf 'VMS_DATABASE_URL=postgres://astra_core:%s@127.0.0.1:5432/astra_vms?sslmode=disable\nVMS_WORKER_SECRET=%s\nASTRA_PUBLIC_HOST=%s\nASTRA_LISTEN=127.0.0.1:18443\n' "$db_password" "$worker_secret" "$public_host" > /etc/astra/core.env
  printf 'VMS_WORKER_SECRET=%s\nASTRA_PUBLIC_HOST=%s\n' "$worker_secret" "$public_host" > /etc/astra/media.env
  # First qualification on the current disk: 160 GiB total media, 40 GiB margin.
  printf '{"path":"/srv/astra/archive","volumeLimit":171798691840,"margin":42949672960}\n' > /var/lib/astra/media/archive-config.json
  chown astra-media:astra-media /var/lib/astra/media/archive-config.json
  chown root:astra-core /etc/astra/core.env
  chown root:astra-media /etc/astra/media.env
  chmod 0640 /etc/astra/core.env /etc/astra/media.env
  printf 'astra-linux-v1\n' > /etc/astra/installation
else
  # Versioned metadata snapshot precedes migrations. It is local staging only.
  "$bundle/backup.sh" "/var/backups/astra/pre-upgrade-$version"
fi
cp -a "$bundle" "$release"
chown -R root:root "$release"
chmod -R a+rX "$release"
chmod 0755 "$release/bin/"* "$release/"*.sh
systemctl stop astra-core.service astra-media.service astra-web.service 2>/dev/null || true
ln -s "$release" /opt/astra/current.next
mv -Tf /opt/astra/current.next /opt/astra/current
install -m 0644 "$release/systemd/"* /etc/systemd/system/
systemctl daemon-reload
systemctl enable astra-web.service astra-media.service astra-core.service
systemctl start astra-web.service astra-media.service astra-core.service
health_authority=$(sed -n 's/^ASTRA_LISTEN=//p' /etc/astra/core.env)
case "$health_authority" in 0.0.0.0:*) health_authority="127.0.0.1:${health_authority##*:}";; esac
[[ $health_authority =~ ^[0-9.]+:[0-9]+$ ]] || { echo 'Unsupported health listener; inspect the configured address.' >&2; exit 1; }
for attempt in $(seq 1 40); do
  if curl -ksf "https://$health_authority/api/v1/health" >/dev/null; then
    printf 'Astra installed: %s. Initial installs use loopback HTTPS; change admin/admin before allowing LAN access.\n' "$version"
    exit 0
  fi
  sleep 1
done
echo 'Health did not become ready. Inspect journalctl -u astra-core; preserve data and schema before recovery.' >&2
exit 1
