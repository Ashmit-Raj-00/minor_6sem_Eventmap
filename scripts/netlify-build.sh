#!/usr/bin/env bash
set -euo pipefail

# Generates a runtime config file for the static frontend.

API_BASE="${EVENTMAP_API_BASE:-}"

cat > web/config.js <<EOF
window.__EVENTMAP_CONFIG__ = {
  apiBase: ${API_BASE@Q},
};
EOF

echo "Generated web/config.js"
