#!/usr/bin/env bash
# Builds the marketing site: src/ -> dist/.
#
# The build is a copy, and that is the whole design. U7 says the site is static
# with a pipeline of its own; a placeholder that needs npm, a bundler and a lock
# file is a placeholder nobody deletes, and the toolchain outlives the excuse for
# it. When the site grows something that genuinely needs building — a templating
# pass, an asset digest — this script grows a step and every caller keeps working.
#
# dist/ is gitignored: it is output, and the repository already decided elsewhere
# that build output is not committed.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
src="$here/src"
dist="$here/dist"

# Clear the CONTENTS rather than the directory. `rm -rf "$dist"` replaces the
# inode, and a running container that bind-mounts it keeps the old one — so the
# site silently empties for everybody until the container is recreated. Found
# exactly that way.
mkdir -p "$dist"
find "$dist" -mindepth 1 -delete
cp -R "$src"/. "$dist"/

printf 'site: built %s -> %s (%s files)\n' \
  "${src#"$PWD"/}" "${dist#"$PWD"/}" "$(find "$dist" -type f | wc -l | tr -d ' ')"
