#!/bin/sh
# Nightly logical backup kept 14 days; copy ./backups to a second server (rsync/rclone) from cron.
# For point-in-time recovery at larger scale use pgBackRest (see docs/OPERATIONS.md).
set -e
cd "$(dirname "$0")"
f=backups/setu-$(date +%Y%m%d-%H%M).dump
docker compose exec -T db pg_dump -U setu_owner -d setu -Fc > "$f"
find backups -name 'setu-*.dump' -mtime +14 -delete
echo "backup: $f ($(du -h "$f" | cut -f1))"
