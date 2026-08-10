#!/usr/bin/env bash
# List the shipped pages that have no translation yet.
#
# Report-only, and that is the point. Translation lands in batches; a check
# that failed the build on the first untranslated page would mean no batch
# could ever be merged. What it is for is making the remainder visible, so
# "we will finish it later" has a number attached rather than being a feeling.
set -euo pipefail

cd "$(dirname "$0")/.."

# The languages the manual is built in, minus the one it is written in.
languages=(zh)

# Which pages ship is decided in mkdocs.yml, so the exclusions are read from
# there rather than listed again here. A second copy of that list is a second
# thing to update, and it drifts the first time a page is excluded — which it
# did, the day dev-stack.md was added and this script went on counting it as
# untranslated.
excludes=()
while IFS= read -r pattern; do
  excludes+=(! -name "$pattern")
done < <(
  awk '/^exclude_docs:/ {inside=1; next}
       inside && /^[^[:space:]]/ {inside=0}
       inside && NF {gsub(/^[[:space:]]+/, ""); gsub(/\/$/, ""); print}' mkdocs.yml
)

# read into an array without mapfile, which macOS's bash 3.2 does not have
pages=()
while IFS= read -r page; do
  pages+=("$page")
done < <(
  find docs -maxdepth 1 -name '*.md' "${excludes[@]}" ! -name '*.*.md' | sort
)

status=0
for language in "${languages[@]}"; do
  missing=()
  total=0
  for page in "${pages[@]}"; do
    total=$((total + 1))
    translated="${page%.md}.${language}.md"
    [[ -f "$translated" ]] || missing+=("$page")
  done

  translated_count=$((total - ${#missing[@]}))
  echo "${language}: ${translated_count}/${total} pages translated"

  if [[ ${#missing[@]} -gt 0 ]]; then
    words=0
    for page in "${missing[@]}"; do
      words=$((words + $(wc -w < "$page")))
      echo "  missing: $page ($(wc -w < "$page" | tr -d ' ') words)"
    done
    echo "  ${words} words remaining in ${language}"
  fi
done

exit $status
