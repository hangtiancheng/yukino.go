#!/bin/sh
set -eu
if [ ! -f /data/sentinel.conf ]; then
  cat > /data/sentinel.conf <<'CONF'
port 26379
bind 0.0.0.0
protected-mode no
dir /data
sentinel resolve-hostnames yes
sentinel announce-hostnames yes
sentinel monitor taskflow redis 6379 2
sentinel down-after-milliseconds taskflow 5000
sentinel failover-timeout taskflow 15000
sentinel parallel-syncs taskflow 1
CONF
fi
exec redis-server /data/sentinel.conf --sentinel
