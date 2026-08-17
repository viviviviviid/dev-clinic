#!/bin/bash
set -euo pipefail

# Build and run the single local clinic backend.
# Usage: ./run.sh [project-base-directory]

project_dir="${1:-.}"
make build-be
exec ./bin/clinic "$project_dir"
