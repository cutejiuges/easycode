#!/bin/sh
set -eu

# 分支名称只从显式参数、可信 CI 源分支变量或本地 symbolic ref 获取。
if [ "$#" -gt 1 ]; then
	printf '%s\n' 'usage: check-branch-name.sh [branch-name]' >&2
	exit 2
fi

branch_name=${1-}
if [ -z "$branch_name" ]; then
	if [ -n "${GITHUB_HEAD_REF-}" ]; then
		branch_name=$GITHUB_HEAD_REF
	elif [ -n "${CI_MERGE_REQUEST_SOURCE_BRANCH_NAME-}" ]; then
		branch_name=$CI_MERGE_REQUEST_SOURCE_BRANCH_NAME
	else
		branch_name=$(git symbolic-ref --quiet --short HEAD 2>/dev/null || true)
	fi
fi

if [ -z "$branch_name" ]; then
	printf '%s\n' 'error: branch name is unavailable; switch from detached HEAD to a semantic branch' >&2
	exit 1
fi

case "$branch_name" in
	main|master)
		printf 'error: direct development on protected branch %s is not allowed\n' "$branch_name" >&2
		exit 1
		;;
esac

pattern='^(feat|fix|refactor|docs|test|ci|build|perf|chore|revert)/[a-z0-9]+(-[a-z0-9]+)*$'
if ! printf '%s\n' "$branch_name" | grep -Eq "$pattern"; then
	printf 'error: branch %s must match %s\n' "$branch_name" "$pattern" >&2
	exit 1
fi

suffix=${branch_name#*/}
if printf '%s\n' "$suffix" | grep -Eq '^([0-9]+|[a-z]+-[0-9]+)$'; then
	printf 'error: branch %s needs a semantic suffix, not only a ticket identifier\n' "$branch_name" >&2
	exit 1
fi

printf 'Branch name is valid: %s\n' "$branch_name"
