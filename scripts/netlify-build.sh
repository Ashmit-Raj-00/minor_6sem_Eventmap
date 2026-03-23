#!/usr/bin/env bash
set -euo pipefail

# Generates a runtime config file for the static frontend.
# Note: SUPABASE_ANON_KEY is public (used by browsers). Do NOT put SUPABASE_JWT_SECRET here.

API_BASE="${EVENTMAP_API_BASE:-}"
SUPABASE_URL="${SUPABASE_URL:-}"
SUPABASE_ANON_KEY="${SUPABASE_ANON_KEY:-}"

cat > web/config.js <<EOF
window.__EVENTMAP_CONFIG__ = {
  apiBase: ${API_BASE@Q},
  supabaseUrl: ${SUPABASE_URL@Q},
  supabaseAnonKey: ${SUPABASE_ANON_KEY@Q},
};
EOF

echo "Generated web/config.js"

