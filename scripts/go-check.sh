#!/usr/bin/env bash
set -euo pipefail

usage() {
cat <<'EOF'
Usage:
  scripts/go-check.sh list
  scripts/go-check.sh test [go test flags ...]
  scripts/go-check.sh vet  [go vet flags ...]

The package set is discovered from tracked .go files and each directory is
resolved explicitly with `go list`; this includes underscore-prefixed route
directories that `go test ./...` omits. Test and vet flags are forwarded.
EOF
}

if [[ $# -eq 0 ]]; then
	usage >&2
	exit 2
fi

command=$1
shift
if [[ "$command" == "-h" || "$command" == "--help" ]]; then
	if [[ $# -ne 0 ]]; then
		printf 'go-check: help cannot be combined with other arguments\n' >&2
		usage >&2
		exit 2
	fi
	usage
	exit 0
fi
case "$command" in
	list|test|vet) ;;
	*)
		printf 'go-check: unknown command %q\n' "$command" >&2
		usage >&2
		exit 2
		;;
esac

if [[ "$command" == "list" && $# -ne 0 ]]; then
	printf 'go-check: list does not accept flags\n' >&2
	usage >&2
	exit 2
fi

repo_root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

# Start with tracked source paths only. This deliberately ignores untracked
# dist/build output and any generated directory that is not in the index.
tracked_dirs=()
while IFS= read -r -d '' path; do
	if [[ "$path" == */* ]]; then
		tracked_dirs+=("${path%/*}")
	else
		tracked_dirs+=(".")
	fi
done < <(git ls-files -z -- '*.go')

if [[ ${#tracked_dirs[@]} -eq 0 ]]; then
	printf 'go-check: no tracked Go source files found\n' >&2
	exit 1
fi

sorted_dirs=()
while IFS= read -r dir; do
	sorted_dirs+=("$dir")
done < <(printf '%s\n' "${tracked_dirs[@]}" | LC_ALL=C sort -u)

# Resolve every source directory as its own go list argument. Do not replace
# this with ./...: Go's recursive pattern intentionally skips underscore
# directories such as app/help/_topic_id.
packages=()
for dir in "${sorted_dirs[@]}"; do
	if [[ "$dir" == "." ]]; then
		list_arg="."
	else
		list_arg="./$dir"
	fi
	mapfile_output=$(go list "$list_arg") || {
		printf 'go-check: go list failed for %q\n' "$list_arg" >&2
		exit 1
	}
	package_lines=()
	while IFS= read -r package; do
		[[ -n "$package" ]] && package_lines+=("$package")
	done <<< "$mapfile_output"
	if [[ ${#package_lines[@]} -ne 1 ]]; then
		printf 'go-check: %q resolved to %d packages; refusing ambiguous coverage\n' "$list_arg" "${#package_lines[@]}" >&2
		exit 1
	fi
	packages+=("${package_lines[0]}")
done

sorted_packages=()
while IFS= read -r package; do
	sorted_packages+=("$package")
done < <(printf '%s\n' "${packages[@]}" | LC_ALL=C sort -u)

if [[ ${#sorted_packages[@]} -ne ${#packages[@]} ]]; then
	printf 'go-check: tracked source directories resolved to duplicate Go packages\n' >&2
	exit 1
fi

case "$command" in
	list)
		printf '%s\n' "${sorted_packages[@]}"
		;;
	test)
		go test "${sorted_packages[@]}" "$@"
		;;
	vet)
		go vet "$@" "${sorted_packages[@]}"
		;;
esac
