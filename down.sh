#!/usr/bin/env bash
set -e

echo "🛑 Stopping Medium Harvester..."
docker compose down --remove-orphans

echo "🧹 Cleaning up Docker resources..."
docker compose rm -f 2>/dev/null || true

# Optional: remove built images
if [ "$1" = "--clean" ]; then
  echo "🗑️  Removing images..."
  docker rmi medium-harvester-frontend medium-harvester-backend 2>/dev/null || true
  echo "🗑️  Pruning dangling images..."
  docker image prune -f
fi

echo "✅ All services stopped."
echo ""
echo "Tip: run './down.sh --clean' to also remove built images."
