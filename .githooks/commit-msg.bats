#!/usr/bin/env bats

setup() {
	bats_require_minimum_version 1.7.0
	hook=$BATS_TEST_DIRNAME/commit-msg
	work=$BATS_TEST_TMPDIR

	# Isolate each test from user configuration and any enclosing Git hook.
	unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR
	unset GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES
	unset GIT_CONFIG GIT_CONFIG_PARAMETERS GIT_CONFIG_COUNT
	export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
	export GIT_TERMINAL_PROMPT=0 LC_ALL=C

	git init -q "$work/repo"
	cd "$work/repo" || return 1
	git config user.name 'Hook Test'
	git config user.email hook-test@example.invalid
	git config commit.gpgSign false
	mkdir .githooks "$work/bin"
	cp "$hook" .githooks/commit-msg
	[ -x .githooks/commit-msg ]
	git config core.hooksPath .githooks

	message=$work/repo/message
	expected=$work/expected
	printf 'hooks: test sanitization\n\nCo-Authored-By: Example\n' > "$message"
}

teardown() {
	# Restore permissions even if a permission test fails or is skipped.
	chmod -R u+rwX "$work" || return 1
	local leftover
	for leftover in "$work/repo"/.commit-msg.* "$work/repo/.git"/.commit-msg.*; do
		if [ -e "$leftover" ]; then
			printf 'temporary files left behind: %s\n' "$leftover" >&2
			return 1
		fi
	done
}

run_hook() {
	if [ -n "${HOOK_SHELL:-}" ]; then
		"$HOOK_SHELL" "$hook" "$@"
	else
		"$hook" "$@"
	fi
}

check_message() {
	local pass input=${1:-$message}
	for ((pass = 1; pass <= 2; pass++)); do
		run --separate-stderr run_hook "$input"
		[ "$status" -eq 0 ]
		[ -z "$output" ]
		[ -z "$stderr" ]
		diff -u -- "$expected" "$input"
	done
}

expect_failure() {
	cp "$message" "$work/before"
	run --separate-stderr "$@"
	[ "$status" -ne 0 ]
	[[ "$stderr" == *"commit-msg:"* ]]
	cmp "$work/before" "$message"
}

fail_utility() {
	printf '#!/bin/sh\nexit 1\n' > "$work/bin/$1"
	chmod +x "$work/bin/$1"
}

@test "body mentions without trailers remain unchanged" {
	cat > "$message" <<'EOF'
hooks: mention Co-Authored-By in the subject

Discuss Co-Authored-By: attribution in ordinary prose.
Co-Authored-By: this is an example inside a body paragraph.
The paragraph continues here.
EOF
	cp "$message" "$expected"
	check_message
}

@test "subject without final newline remains unchanged" {
	printf 'hooks: no final newline' > "$message"
	cp "$message" "$expected"
	check_message
}

@test "empty message remains for Git to validate" {
	: > "$message"
	cp "$message" "$expected"
	check_message
}

@test "one co-author trailer is removed" {
	cat > "$message" <<'EOF'
hooks: remove a trailer

Keep the body.

Co-Authored-By: Example <example@example.invalid>
EOF
	cat > "$expected" <<'EOF'
hooks: remove a trailer

Keep the body.

EOF
	check_message
}

@test "multiple case variants and empty co-author values are removed" {
	cat > "$message" <<'EOF'
hooks: remove repeated trailers

CO-AUTHORED-BY: First <first@example.invalid>
co-authored-by: Second <second@example.invalid>
Co-AuThOrEd-By :
EOF
	printf 'hooks: remove repeated trailers\n\n' > "$expected"
	check_message
}

@test "mixed trailers and continuations preserve original formatting" {
	cat > "$message" <<'EOF'
hooks: preserve mixed trailers

Co-Authored-By: an example in the body
This sentence keeps the example in a prose paragraph.

Reviewed-by :  Reviewer <reviewer@example.invalid>
  preserve this folded review
Co-Authored-By: First <first@example.invalid>
  remove this continuation
	and this tabbed continuation
Acked-by:
co-authored-by: Second <second@example.invalid>
  remove this too
Signed-off-by: Signer <signer@example.invalid>
EOF
	cat > "$expected" <<'EOF'
hooks: preserve mixed trailers

Co-Authored-By: an example in the body
This sentence keeps the example in a prose paragraph.

Reviewed-by :  Reviewer <reviewer@example.invalid>
  preserve this folded review
Acked-by:
Signed-off-by: Signer <signer@example.invalid>
EOF
	check_message
}

@test "trailer-shaped subject remains unchanged" {
	cat > "$message" <<'EOF'
Co-Authored-By: subject text

Reviewed-by: Reader
EOF
	cp "$message" "$expected"
	check_message
}

@test "comments inside and after the footer remain unchanged" {
	cat > "$message" <<'EOF'
hooks: preserve comments

Co-Authored-By: Example
  remove folded value
# Keep this comment between trailers.
Signed-off-by: Signer

# Co-Authored-By: a comment, not a trailer
# Please enter the commit message.
EOF
	cat > "$expected" <<'EOF'
hooks: preserve comments

# Keep this comment between trailers.
Signed-off-by: Signer

# Co-Authored-By: a comment, not a trailer
# Please enter the commit message.
EOF
	check_message
}

@test "non-trailer text in a Git-recognized block remains unchanged" {
	cat > "$message" <<'EOF'
hooks: preserve non-trailer lines

Signed-off-by: Signer
Keep this non-trailer line in the recognized trailer block.
Co-Authored-By: Example
EOF
	cat > "$expected" <<'EOF'
hooks: preserve non-trailer lines

Signed-off-by: Signer
Keep this non-trailer line in the recognized trailer block.
EOF
	check_message
}

@test "body divider does not hide trailers" {
	cat > "$message" <<'EOF'
hooks: allow a divider in prose

---
This is part of the body.

Co-Authored-By: Example
EOF
	cat > "$expected" <<'EOF'
hooks: allow a divider in prose

---
This is part of the body.

EOF
	check_message
}

@test "scissors and verbose diff remain untouched" {
	cat > "$message" <<'EOF'
hooks: preserve verbose commit content

Co-Authored-By: Example
# ------------------------ >8 ------------------------
Co-Authored-By: ignored after scissors
diff --git a/example b/example
--- a/example
+++ b/example
EOF
	cat > "$expected" <<'EOF'
hooks: preserve verbose commit content

# ------------------------ >8 ------------------------
Co-Authored-By: ignored after scissors
diff --git a/example b/example
--- a/example
+++ b/example
EOF
	check_message
}

@test "CRLF and whitespace are preserved" {
	printf 'hooks: preserve bytes\r\n\r\nBody with a tab\tand spaces  \r\n\r\nCo-Authored-By: Example\r\n\tfolded value\r\nReviewed-by :  Reader\r\n' > "$message"
	printf 'hooks: preserve bytes\r\n\r\nBody with a tab\tand spaces  \r\n\r\nReviewed-by :  Reader\r\n' > "$expected"
	check_message
}

@test "mixed footer without final newline is preserved" {
	printf 'hooks: preserve final bytes\n\nCo-Authored-By: Example\nReviewed-by: Reader' > "$message"
	printf 'hooks: preserve final bytes\n\nReviewed-by: Reader' > "$expected"
	check_message
}

@test "co-author trailer without final newline is removed" {
	printf 'hooks: remove final bytes\n\nCo-Authored-By: Example' > "$message"
	printf 'hooks: remove final bytes\n\n' > "$expected"
	check_message
}

@test "configured aliases, commands, and separators do not rewrite the message" {
	git config trailer.co-authored-by.key 'Renamed-Key'
	git config trailer.review.cmd 'touch unexpected-command; echo unwanted'
	git config trailer.separators '='
	printf 'hooks: ignore trailer rewrites\n\nCo-Authored-By: Example\nReviewed-by : Reader\n' > "$message"
	printf 'hooks: ignore trailer rewrites\n\nReviewed-by : Reader\n' > "$expected"
	check_message
	[ ! -e unexpected-command ]
}

@test "custom comment character is respected" {
	git config core.commentChar ';'
	printf 'hooks: custom comments\n\nCo-Authored-By: Example\n\n; Keep this comment.\n' > "$message"
	printf 'hooks: custom comments\n\n\n; Keep this comment.\n' > "$expected"
	check_message
}

@test "absolute and relative message paths with spaces and a leading dash work" {
	local message_with_spaces="$work/repo/-message with spaces"
	mv "$message" "$message_with_spaces"
	printf 'hooks: test sanitization\n\n' > "$expected"
	check_message "$message_with_spaces"
	printf 'hooks: test sanitization\n\nCo-Authored-By: Example\n' > "$message_with_spaces"
	check_message '-message with spaces'
}

@test "missing argument is rejected" {
	expect_failure run_hook
}

@test "missing file is rejected" {
	expect_failure run_hook "$work/missing"
}

@test "directory input is rejected" {
	expect_failure run_hook "$work/repo"
}

@test "symlink input is rejected without changing its target" {
	ln -s "$message" "$work/link"
	expect_failure run_hook "$work/link"
}

@test "NUL input is rejected without changing the message" {
	printf 'hooks: reject NUL\n\nCo-Authored-By: Example\000hidden\n' > "$message"
	expect_failure run_hook "$message"
}

@test "newly exposed footer is rejected without changing the message" {
	printf 'hooks: ambiguous footer\n\nCo-Authored-By: body example\n\nCo-Authored-By: footer\n' > "$message"
	expect_failure run_hook "$message"
}

@test "read-only message is rejected" {
	chmod a-w "$message"
	[ ! -w "$message" ] || skip 'current user can write read-only files'
	expect_failure run_hook "$message"
}

@test "unreadable message is rejected" {
	cp "$message" "$work/before"
	chmod a-r "$message"
	[ ! -r "$message" ] || skip 'current user can read unreadable files'
	run --separate-stderr run_hook "$message"
	[ "$status" -ne 0 ]
	[[ "$stderr" == *"commit-msg:"* ]]
	chmod u+r "$message"
	cmp "$work/before" "$message"
}

@test "Git parser failure leaves the original message intact" {
	fail_utility git
	expect_failure env PATH="$work/bin:$PATH" "$hook" "$message"
}

@test "temporary directory failure leaves the original message intact" {
	fail_utility mktemp
	expect_failure env PATH="$work/bin:$PATH" "$hook" "$message"
}

@test "snapshot failure leaves the original message intact" {
	fail_utility cp
	expect_failure env PATH="$work/bin:$PATH" "$hook" "$message"
}

@test "line marking failure leaves the original message intact" {
	fail_utility awk
	expect_failure env PATH="$work/bin:$PATH" "$hook" "$message"
}

@test "replacement failure leaves the original message intact" {
	fail_utility mv
	expect_failure env PATH="$work/bin:$PATH" "$hook" "$message"
}

@test "replacement write failure leaves the original message intact" {
	local real_awk
	real_awk=$(command -v awk)
	cat > "$work/bin/awk" <<'EOF'
#!/bin/sh
case $2 in
	bytes=*) exit 1 ;;
esac
exec "$REAL_AWK" "$@"
EOF
	chmod +x "$work/bin/awk"
	expect_failure env PATH="$work/bin:$PATH" REAL_AWK="$real_awk" "$hook" "$message"
}

@test "Git runs the hook for a normal commit" {
	cat > "$message" <<'EOF'
hooks: create a normal commit

Keep this commit body.

Co-Authored-By: Example <example@example.invalid>
  folded attribution
Reviewed-by: Reader <reader@example.invalid>
EOF
	cat > "$expected" <<'EOF'
hooks: create a normal commit

Keep this commit body.

Reviewed-by: Reader <reader@example.invalid>
EOF
	run git -c commit.cleanup=strip commit -q --allow-empty -F "$message"
	[ "$status" -eq 0 ]
	git log -1 --format=format:%B > "$work/committed"
	diff -u "$expected" "$work/committed"
}

@test "Git runs the hook for an amended commit" {
	git commit -q --allow-empty -m 'hooks: initial commit'
	local first
	first=$(git rev-parse HEAD)
	cat > "$message" <<'EOF'
hooks: amend a commit

Mention Co-Authored-By in prose.

Co-Authored-By: First <first@example.invalid>
co-authored-by: Second <second@example.invalid>
  folded attribution
Signed-off-by: Signer <signer@example.invalid>
EOF
	cat > "$expected" <<'EOF'
hooks: amend a commit

Mention Co-Authored-By in prose.

Signed-off-by: Signer <signer@example.invalid>
EOF
	run git -c commit.cleanup=strip commit -q --amend --allow-empty -F "$message"
	[ "$status" -eq 0 ]
	[ "$(git rev-parse HEAD)" != "$first" ]
	git log -1 --format=format:%B > "$work/committed"
	diff -u "$expected" "$work/committed"
}

@test "Git preserves the sanitized message when amending without editing" {
	printf 'hooks: amend without editing\n\nCo-Authored-By: Example\nReviewed-by: Reader\n' > "$message"
	printf 'hooks: amend without editing\n\nReviewed-by: Reader\n' > "$expected"
	git -c commit.cleanup=strip commit -q --allow-empty -F "$message"
	run git commit -q --amend --allow-empty --no-edit
	[ "$status" -eq 0 ]
	git log -1 --format=format:%B > "$work/committed"
	diff -u "$expected" "$work/committed"
}

@test "Git rejects the commit when sanitization fails" {
	git commit -q --allow-empty -m 'hooks: initial commit'
	local first
	first=$(git rev-parse HEAD)
	fail_utility mv
	run --separate-stderr env PATH="$work/bin:$PATH" \
		git commit -q --allow-empty -F "$message"
	[ "$status" -ne 0 ]
	[[ "$stderr" == *"commit-msg:"* ]]
	[ "$(git rev-parse HEAD)" = "$first" ]
}
