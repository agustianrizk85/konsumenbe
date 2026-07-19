#!/usr/bin/env bash
# Deploy backend Konsumen: tarik kode terbaru, build, jalankan/-ulang via PM2.
# Jalankan di server dari dalam folder repo: ./deploy.sh
#
#   cd /opt/apps && git clone git@github.com:agustianrizk85/konsumenbe.git
#   cd konsumenbe && cp konsumen.env.example /opt/apps/konsumen.env  # lalu isi
#   ./deploy.sh
#
# API dijangkau nginx lewat path /be/konsumen -> 127.0.0.1:${KONSUMEN_PORT:-8092}.
set -euo pipefail
cd "$(dirname "$0")"

echo "==> git pull"
git pull --ff-only

echo "==> go build"
export PATH="$PATH:/usr/local/go/bin"
CGO_ENABLED=0 go build -trimpath -o konsumen-server ./cmd/server

# Muat env (DB, JWKS, Sales API, port, folder upload) dari file di luar git bila ada.
set -a; [ -f /opt/apps/konsumen.env ] && . /opt/apps/konsumen.env; set +a

echo "==> (re)start PM2: konsumen-be (port ${KONSUMEN_PORT:-8092})"
pm2 restart konsumen-be --update-env 2>/dev/null || pm2 start ./konsumen-server --name konsumen-be --update-env
pm2 save
echo "==> selesai. status:"
pm2 status konsumen-be
