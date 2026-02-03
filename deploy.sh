#!/usr/bin/env bash
set -euo pipefail

cd /mnt/storage/apps/debt-manager

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

echo "==> Health"
curl -fsS -I http://127.0.0.1:4005/ >/dev/null
echo "Deploy OK"
