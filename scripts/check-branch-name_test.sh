#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
checker=$script_dir/check-branch-name.sh

expect_pass() {
	name=$1
	shift
	if ! "$@" >/dev/null 2>&1; then
		printf 'expected branch case to pass: %s\n' "$name" >&2
		exit 1
	fi
}

expect_fail() {
	name=$1
	shift
	if "$@" >/dev/null 2>&1; then
		printf 'expected branch case to fail: %s\n' "$name" >&2
		exit 1
	fi
}

for prefix in feat fix refactor docs test ci build perf chore revert; do
	expect_pass "$prefix" "$checker" "$prefix/semantic-branch-name"
done

for branch in \
	main master codex/architecture-fix cutejiuge/architecture-fix \
	feature/add-tools fix/has_underscore fix/UPPERCASE fix/-leading \
	fix/trailing- fix/double--dash fix/123 fix/ec-123 fix/; do
	expect_fail "$branch" "$checker" "$branch"
done

expect_pass ci-source env GITHUB_HEAD_REF=fix/ci-source "$checker"
expect_fail ci-invalid env GITHUB_HEAD_REF=refs/pull/7/merge "$checker"
expect_pass explicit-overrides-ci env GITHUB_HEAD_REF=refs/pull/7/merge "$checker" feat/explicit-source
expect_pass gitlab-source env CI_MERGE_REQUEST_SOURCE_BRANCH_NAME=docs/branch-policy "$checker"

temporary_root=$(mktemp -d)
trap 'rm -rf -- "$temporary_root"' EXIT HUP INT TERM
(
	# Git hook 会导出当前仓库的本地环境变量，隔离后才能真实构造临时 detached HEAD。
	unset GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_CONFIG GIT_CONFIG_PARAMETERS \
		GIT_CONFIG_COUNT GIT_OBJECT_DIRECTORY GIT_DIR GIT_WORK_TREE \
		GIT_IMPLICIT_WORK_TREE GIT_GRAFT_FILE GIT_INDEX_FILE GIT_NO_REPLACE_OBJECTS \
		GIT_REPLACE_REF_BASE GIT_PREFIX GIT_SHALLOW_FILE GIT_COMMON_DIR
	git -C "$temporary_root" init -q
	git -C "$temporary_root" -c user.name=Fixture -c user.email=fixture@example.invalid \
		commit --allow-empty -q -m fixture
	git -C "$temporary_root" checkout --detach -q HEAD
	expect_fail detached-head env GITHUB_HEAD_REF= CI_MERGE_REQUEST_SOURCE_BRANCH_NAME= \
		sh -c 'cd "$1" && "$2"' sh "$temporary_root" "$checker"
)

printf '%s\n' 'Branch name policy tests passed.'
