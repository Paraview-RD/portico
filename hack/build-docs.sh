#!/usr/bin/env bash
# Build the documentation that is embedded in the binary.
#
# mkdocs cleans its site_dir before writing, which removes the .gitkeep that
# keeps `go:embed all:site` compiling in a fresh clone. So it is put back
# afterwards — that is the entire reason this is a script rather than one
# line in a README.
set -euo pipefail

cd "$(dirname "$0")/.."

mkdocs build --strict "$@"
touch internal/docs/site/.gitkeep

echo "docs built into internal/docs/site"
