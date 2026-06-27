---
layout: default
title: Deployment
---
# Deployment

## Single host (simplest)

```bash
# 1. Build
make build

# 2. Copy to the server
scp ./bin/ttl user@server:/usr/local/bin/

# 3. Create a system user and data dir
ssh user@server '
  sudo useradd -r -s /bin/false ttl
  sudo install -d -o ttl -g ttl -m 0700 /var/lib/ttl
'

# 4. systemd unit
cat <<'EOF' | sudo tee /etc/systemd/system/ttl.service
[Unit]
Description=ttl task tracker
After=network.target

[Service]
Type=simple
User=ttl
ExecStart=/usr/local/bin/ttl serve --addr :8093 --db /var/lib/ttl/ttl.db
Restart=on-failure
RestartSec=5
Environment=TTL_DATA_DIR=/var/lib/ttl

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now ttl
```

## Docker (distroless)

The provided Dockerfile produces a ~20 MB image based on
`gcr.io/distroless/static-debian12:nonroot`.

```bash
make docker                       # tags ttl:dev
docker run -d --name ttl \
  -p 8080:8093 \
  -v ttl-data:/data \
  ttl:dev
```

A `docker-compose.yml` you can drop next to your app:

```yaml
version: "3.8"
services:
  ttl:
    image: ttl:0.1.0
    restart: unless-stopped
    ports:
      - "8080:8093"
    volumes:
      - ttl-data:/data
    environment:
      - TTL_DATA_DIR=/data
volumes:
  ttl-data:
```

## Reverse proxy + TLS

Caddy:

```
ttl.example.com {
    reverse_proxy localhost:8093
}
```

nginx:

```nginx
server {
    listen 443 ssl http2;
    server_name ttl.example.com;

    ssl_certificate     /etc/letsencrypt/live/ttl.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/ttl.example.com/privkey.pem;

    location / {
        proxy_pass         http://localhost:8093;
        proxy_http_version 1.1;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;

        # WebSocket
        proxy_set_header   Upgrade           $http_upgrade;
        proxy_set_header   Connection        "upgrade";
        proxy_read_timeout 600s;
    }
}
```

## Backups

The database is a single SQLite file. `cp` works:

```bash
sqlite3 /var/lib/ttl/ttl.db ".backup /backups/ttl-$(date +%F).db"
```

Or via the standard online backup API. For WAL-mode databases, also
copy the `.db-wal` and `.db-shm` sidecar files (or take the backup
through the `.backup` command which handles this atomically).

Schedule with cron or systemd-timer.

## Scaling

Phase 1–3 target single-user or small-team workloads on a single
machine. SQLite + WAL handles hundreds of writes per second. The WebSocket
hub keeps state in-process — for >1 instance behind a load balancer
you'd need to swap that for Redis pub/sub (the `events.Hub` interface
already isolates the in-process implementation).

## Production checklist

- [ ] Run behind TLS (Caddy/nginx/Traefik).
- [ ] Set up automated daily backups of `~/.local/share/ttl/ttl.db`.
- [ ] Run `ttl serve` as a non-root user; make the data dir mode 0700.
- [ ] If exposing publicly, add rate limiting at the proxy layer.
- [ ] Monitor disk usage; the SQLite file grows but `VACUUM` periodically
      shrinks it.
- [ ] Set TTL_DATA_DIR and TTL_CONFIG_DIR explicitly in the systemd unit
      so multiple deployments can share a machine.

## Troubleshooting

**"connection refused" on `ttl signup`**

The server isn't running on the configured port. Either start it
(`ttl serve`) or point the CLI at the right URL:
`ttl signup --server http://hostname:8093`.

**WebSocket disconnects every minute**

Some reverse proxies time out idle connections. Set the proxy
`proxy_read_timeout` to at least 120s; or upgrade to a proxy that
respects the WebSocket pings (Caddy does this by default).

**Reminders not firing on the server**

Check `--reminder-interval` and the server's stderr. The ticker also
publishes events to the hub; if no WebSocket clients are connected
the events are dropped (by design). Local OS notifications fire
regardless.
