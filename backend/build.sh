#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")"

# Copy frontend files into site handler for embedding
mkdir -p cmd/site/static
cp ../frontend/index.html cmd/site/static/
cp ../frontend/style.css cmd/site/static/
cp ../frontend/ephemeral.js cmd/site/static/

for cmd in upload session stream heartbeat complete check site; do
    echo "Building $cmd..."
    GOOS=linux GOARCH=arm64 go build -o "dist/$cmd/bootstrap" "./cmd/$cmd"
    (cd "dist/$cmd" && zip "../$cmd.zip" bootstrap)
done

echo "Done. Artifacts in backend/dist/"
