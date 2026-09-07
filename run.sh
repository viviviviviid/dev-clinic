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
# Resolve relative paths before entering the repository to build and read the
# optional local overrides. A supplied directory always wins over BASE_DIR.
if [[ "$project_dir" != /* && "$project_dir" != '~' && "$project_dir" != '~/'* ]]; then
  project_dir="$PWD/$project_dir"
fi
cd "$script_dir"
make build-be
exec "$script_dir/bin/clinic" "$project_dir"
