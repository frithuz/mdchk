#!/bin/sh
# Called from the Taskfile directory; never install tools or edit global config.
set -eu

die() {
	printf 'repo:init: %s\n' "$*" >&2
	exit 1
}

# Check the hook's prerequisites before activating it, too.
for tool in git awk cmp cp mv tr wc mktemp rm; do
	command -v "$tool" >/dev/null 2>&1 || die "required tool not found: $tool"
done

[ "$(git rev-parse --is-inside-work-tree 2>/dev/null)" = true ] ||
	die 'run inside a non-bare Git checkout'
[ -z "$(git rev-parse --show-prefix)" ] ||
	die 'run from the repository root (use task repo:init)'
[ -f .githooks/commit-msg ] && [ ! -L .githooks/commit-msg ] &&
	[ -x .githooks/commit-msg ] ||
	die 'required hook .githooks/commit-msg must be a regular executable file'
git interpret-trailers --parse --no-divider </dev/null >/dev/null 2>&1 ||
	die 'Git 2.22 or newer with interpret-trailers --parse is required'

if previous=$(git config --get core.hooksPath); then
	if [ "$previous" != .githooks ]; then
		printf 'repo:init: replacing core.hooksPath (%s) with .githooks for this repository\n' "$previous" >&2
	fi
else
	status=$?
	[ "$status" -eq 1 ] || die 'cannot read core.hooksPath'
fi

git config --local core.hooksPath .githooks ||
	die 'cannot activate hooks in repository-local Git configuration'
printf 'repo:init: repository Git hooks activated\n'
