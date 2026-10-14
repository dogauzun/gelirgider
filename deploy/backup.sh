#!/bin/sh
# Back up gelirgider's SQLite database.
#
# Runs the backend's own -backup flag (VACUUM INTO under the hood) for a
# consistent snapshot even with WAL active, gzips it into
# deploy/data/backups/, and deletes local backups older than 14 days.
#
# Copying the backups directory somewhere off this host (another disk,
# object storage, whatever sync tool is already in use) is the operator's
# job; this script only manages local copies.
#
# Cron example (daily at 03:30 host time), using the absolute path to this
# script and log file for your checkout:
#   30 3 * * * /path/to/gelirgider/deploy/backup.sh >> /path/to/gelirgider/deploy/data/backup.log 2>&1
#
# Run it as root (sudo, root's crontab): deploy/data is owned by the
# backend's uid 65532 with mode 700, so no other host user can reach it.
# Backups hold password hashes and encrypted provider keys: everything this
# script creates is readable by its owner only.
set -eu
umask 077

cd "$(dirname "$0")"

ts=$(date +%Y%m%dT%H%M%S)
backup_dir="./data/backups"
container_file="/data/backup-${ts}.db"
host_file="./data/backup-${ts}.db"

mkdir -p "$backup_dir"
chmod 700 "$backup_dir"

docker compose -f compose.yml exec -T backend /server -backup "$container_file"

mv "$host_file" "$backup_dir/gelirgider-${ts}.db"
# The backend creates the snapshot 0600; this keeps it so for an image built
# before GLG-1128, which wrote it under its umask. gzip copies the mode.
chmod 600 "$backup_dir/gelirgider-${ts}.db"
gzip "$backup_dir/gelirgider-${ts}.db"

find "$backup_dir" -name '*.db.gz' -type f -mtime +14 -delete

echo "backup ok: $backup_dir/gelirgider-${ts}.db.gz"
