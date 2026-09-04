#!/bin/bash
set -euo pipefail

# Build and run the single local clinic backend.
# Usage: ./run.sh [project-base-directory]
# Defaults to the repository-local data directory.

script_dir="$(cd "$(dirname "$0")" && pwd)"
if [[ -f "$script_dir/.env.local" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$script_dir/.env.local"
  set +a
fi

project_dir="${1:-$script_dir/data}"
make build-be
exec ./bin/clinic "$project_dir"
