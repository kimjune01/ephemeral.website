#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")"

for cmd in upload session stream heartbeat complete; do
    echo "Building $cmd..."
    GOOS=linux GOARCH=arm64 go build -o "dist/$cmd/bootstrap" "./cmd/$cmd"
    (cd "dist/$cmd" && zip "../$cmd.zip" bootstrap)
done

echo "Done. Artifacts in backend/dist/"
