#!/usr/bin/env bash
# Start the server, on every start of the Codespace rather than only on the
# first — a machine that has been stopped and resumed still has its database
# and its build, and would otherwise come back with nothing listening.
set -euo pipefail

cd "$(dirname "$0")/.."

# PORTICO_PUBLIC_URL is the one setting a Codespace cannot inherit from the
# compose file, and the one that matters most for what this environment is
# for. OpenID Connect and SAML build their redirect targets and their
# metadata from it, so a value of localhost gives a console that works
# perfectly and every single-sign-on flow sending the browser somewhere it
# cannot reach. Computed from what Codespaces sets, and falling back to
# localhost so the same devcontainer opens on a laptop.
if [ -n "${CODESPACE_NAME:-}" ]; then
  domain="${GITHUB_CODESPACES_PORT_FORWARDING_DOMAIN:-app.github.dev}"
  export PORTICO_PUBLIC_URL="https://${CODESPACE_NAME}-8410.${domain}"
else
  export PORTICO_PUBLIC_URL="http://localhost:8410"
fi

if [ ! -x ./portico ]; then
  echo "No binary yet — setup.sh has not finished. Skipping start." >&2
  exit 0
fi

# Already listening: a second one would fail on the port and leave a
# confusing log behind.
if curl -sf -o /dev/null http://localhost:8410/api/v1/health 2>/dev/null; then
  echo "Portico is already running at ${PORTICO_PUBLIC_URL}"
  exit 0
fi

nohup ./portico >/tmp/portico.log 2>&1 &

for _ in $(seq 1 30); do
  if curl -sf -o /dev/null http://localhost:8410/api/v1/health 2>/dev/null; then
    echo "Portico is at ${PORTICO_PUBLIC_URL}"
    exit 0
  fi
  sleep 1
done

echo "Portico did not answer within 30s. The log is /tmp/portico.log:" >&2
tail -20 /tmp/portico.log >&2
exit 1
