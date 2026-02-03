#!/usr/bin/env bash
set -euo pipefail

cd /mnt/storage/apps/debt-manager

# Health check port: live app runs on 4005 (set by systemd), not .env
HEALTH_PORT=4005

echo "==> Pull"
git pull

echo "==> Test"
go test ./...

echo "==> Build"
go build -o debtapp.new .

echo "==> Swap"
mv -f debtapp debtapp.prev || true
mv -f debtapp.new debtapp

echo "==> Restart"
sudo systemctl restart debt-manager

echo "==> Health (port ${HEALTH_PORT})"
sleep 3
if ! curl -fsS -o /dev/null -w "%{http_code}" --connect-timeout 5 "http://127.0.0.1:${HEALTH_PORT}/" >/dev/null; then
  echo "Health check failed. Service status:"
  sudo systemctl status debt-manager --no-pager || true
  exit 1
fi
echo "Deploy OK"
