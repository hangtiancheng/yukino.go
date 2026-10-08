#!/usr/bin/env bash
set -euo pipefail
case "$REPLICATION_PASSWORD" in
  ''|*[!a-zA-Z0-9_-]*) echo "Replication password must contain letters, digits, underscores or hyphens." >&2; exit 1 ;;
esac
primary=(mysql --protocol=TCP -h mysql -uroot --batch --skip-column-names)
replica=(mysql --protocol=TCP -h mysql-replica -uroot --batch --skip-column-names)
channel=$("${replica[@]}" -e 'SHOW REPLICA STATUS')
if [[ -n "$channel" ]]; then
  "${replica[@]}" -e 'START REPLICA;'
  echo "Existing replication channel retained."
else
count=$("${replica[@]}" -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='taskflow'")
if [[ "$count" != 0 ]]; then
  echo "Refusing to overwrite a replica with existing application tables." >&2
  exit 1
fi
"${primary[@]}" <<SQL
CREATE USER IF NOT EXISTS 'replicator'@'%' IDENTIFIED BY '${REPLICATION_PASSWORD}';
ALTER USER 'replicator'@'%' IDENTIFIED BY '${REPLICATION_PASSWORD}';
GRANT REPLICATION SLAVE ON *.* TO 'replicator'@'%';
SQL
mysqldump --protocol=TCP -h mysql -uroot --single-transaction --routines --triggers --source-data=2 --set-gtid-purged=ON --databases taskflow > /tmp/taskflow-snapshot.sql
"${replica[@]}" -e 'SET GLOBAL super_read_only=OFF; RESET BINARY LOGS AND GTIDS;'
"${replica[@]}" < /tmp/taskflow-snapshot.sql
"${replica[@]}" <<SQL
CHANGE REPLICATION SOURCE TO SOURCE_HOST='mysql', SOURCE_USER='replicator', SOURCE_PASSWORD='${REPLICATION_PASSWORD}', SOURCE_AUTO_POSITION=1, GET_SOURCE_PUBLIC_KEY=1, SOURCE_CONNECT_RETRY=1;
START REPLICA;
SQL
fi
"${replica[@]}" -e 'SET PERSIST read_only=ON; SET PERSIST super_read_only=ON;'
for attempt in {1..60}; do
  status=$("${replica[@]}" --column-names -e 'SHOW REPLICA STATUS\G')
  if [[ "$status" == *"Replica_IO_Running: Yes"* && "$status" == *"Replica_SQL_Running: Yes"* ]]; then
    echo "MySQL GTID replication is running."
    exit 0
  fi
  sleep 1
done
echo "MySQL replication did not become healthy." >&2
exit 1
