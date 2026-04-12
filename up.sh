#!/usr/bin/env bash
set -e

echo "🚀 Starting Medium Harvester..."
docker compose up --build -d
echo ""
echo "✅ Services running:"
echo "   Frontend → http://localhost:3000"
echo "   Backend  → http://localhost:8080"
echo ""
echo "📁 Output will be written to ./output/"
