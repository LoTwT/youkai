#!/usr/bin/env bash
# Read release state without relying on /releases/tags/{tag}, which GitHub
# documents as returning a published release only. The paginated list
# endpoint includes drafts for tokens with push access (the release
# workflow's contents: write token), and every entry is validated before a
# missing tag is treated as absent.
set -eu

repo="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
command="${1:-}"
shift || true

fail() {
  echo "release-state: $*" >&2
  exit 1
}

# tag channel -> version on stdout, failing when the tag does not belong to
# the channel or does not use major.minor.patch.
parse_tag() {
  local tag="$1" channel="$2" prefix version
  case "$channel" in
    stable) prefix="v" ;;
    test) prefix="test-v" ;;
    *) fail "unknown channel: $channel" ;;
  esac
  case "$tag" in
    "$prefix"*) version="${tag#"$prefix"}" ;;
    *) fail "release tag $tag does not match the $channel channel" ;;
  esac
  printf '%s' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' ||
    fail "release tag $tag is not ${prefix}<major.minor.patch>"
  printf '%s\n' "$version"
}

# Load one validated, matchable release list. The legal empty-repository
# response is a non-empty array containing pages; [] is not the slurped page
# shape and is rejected. Every entry must carry enough data to match and to
# reason about published state; unrelated tag formats are allowed.
load_releases() {
  local pages
  pages=$(gh api --paginate --slurp "repos/$repo/releases?per_page=100") ||
    fail "cannot list releases for $repo"
  printf '%s\n' "$pages" | jq -e '
    type == "array" and length > 0 and
    all(.[]; type == "array") and
    all(.[][];
      type == "object" and
      ((.tag_name | type) == "string") and ((.tag_name | length) > 0) and
      ((.id | type) == "number") and (.id > 0) and
      ((.draft | type) == "boolean") and
      ((.prerelease | type) == "boolean") and
      ((.assets | type) == "array") and
      all(.assets[];
        type == "object" and
        ((.id | type) == "number") and (.id > 0) and
        ((.name | type) == "string") and ((.name | length) > 0)
      )
    )
  ' >/dev/null || fail "the release list is not a complete, matchable page set"
  all_releases=$(printf '%s\n' "$pages" | jq -c '[.[][]]') ||
    fail "cannot flatten the release list"
}

# Output {"state":"absent"} or {"state":"release","release":{...}}.
target_state() {
  local tag="$1" matches count release
  matches=$(printf '%s\n' "$all_releases" | jq -c --arg tag "$tag" \
    '[.[] | select(.tag_name == $tag)]') ||
    fail "cannot match release $tag"
  count=$(printf '%s\n' "$matches" | jq -r 'length') ||
    fail "cannot count releases for tag $tag"
  case "$count" in
    0)
      printf '{"state":"absent"}\n'
      ;;
    1)
      release=$(printf '%s\n' "$matches" | jq -c '.[0]') ||
        fail "cannot read release $tag"
      printf '{"state":"release","release":%s}\n' "$release"
      ;;
    *)
      fail "multiple releases found for tag $tag"
      ;;
  esac
}

require_draft() {
  local tag="$1" state release draft
  state=$(target_state "$tag") || exit 1
  [ "$(printf '%s\n' "$state" | jq -r '.state')" = "release" ] ||
    fail "there is no release for tag $tag"
  release=$(printf '%s\n' "$state" | jq -c '.release') ||
    fail "cannot read release $tag"
  draft=$(printf '%s\n' "$release" | jq -r '.draft') ||
    fail "cannot read the draft state for tag $tag"
  [ "$draft" = "true" ] || fail "release $tag is already published"
  printf '{"release":%s}\n' "$release"
}

# Query /releases/latest with curl. Curl's exit status proves the transfer
# completed; the HTTP status and body are only interpreted after that. The
# 200 body is read only after a complete transfer; the 404 body is validated
# directly from the completed file before it can authorize a decision. The
# token is supplied through a 0600 curl config file, never through argv.
latest_exchange() {
  local api_base api_timeout expected_size
  : "${GH_TOKEN:?GH_TOKEN is required for the latest request}"
  api_base="${GITHUB_API_URL:-https://api.github.com}"
  api_timeout="${GITHUB_API_TIMEOUT:-30}"
  latest_config=$(mktemp) || fail "cannot create a temporary file"
  latest_body_file=$(mktemp) || fail "cannot create a temporary file"
  latest_headers=$(mktemp) || fail "cannot create a temporary file"
  latest_err=$(mktemp) || fail "cannot create a temporary file"
  trap 'rm -f "$latest_config" "$latest_body_file" "$latest_headers" "$latest_err"' EXIT
  chmod 600 "$latest_config"
  cat > "$latest_config" <<EOF
header = "Authorization: Bearer ${GH_TOKEN}"
EOF
  latest_rc=0
  latest_meta=$(curl --silent --show-error \
    --proto '=https' \
    --connect-timeout 10 \
    --max-time "$api_timeout" \
    --config "$latest_config" \
    --header "Accept: application/vnd.github+json" \
    --header "X-GitHub-Api-Version: 2022-11-28" \
    --user-agent "youkai-release-workflow" \
    --dump-header "$latest_headers" \
    --output "$latest_body_file" \
    --write-out '%{http_code} %{size_download}' \
    "${api_base%/}/repos/$repo/releases/latest" 2>"$latest_err") || latest_rc=$?
  if [ "$latest_rc" -ne 0 ]; then
    cat "$latest_err" >&2
    fail "the latest request did not complete (curl exit $latest_rc)"
  fi
  latest_status="${latest_meta%% *}"
  latest_size="${latest_meta##* }"
  case "$latest_status" in
    [0-9][0-9][0-9]) ;;
    *) fail "the latest request returned no usable HTTP status" ;;
  esac
  case "$latest_size" in
    ''|*[!0-9]*) fail "the latest request returned no usable response size" ;;
  esac
  expected_size=$(awk '
    { sub(/\r$/, "") }
    tolower($1) == "content-length:" { size = $2 }
    END { if (size != "") print size }
  ' "$latest_headers")
  if [ -n "$expected_size" ] && [ "$expected_size" != "$latest_size" ]; then
    fail "the latest response body is incomplete ($latest_size of $expected_size bytes)"
  fi
  latest_body=""
  if [ "$latest_status" = "200" ]; then
    latest_body=$(cat "$latest_body_file") || fail "cannot read the latest response body"
  fi
}

# Validate the body of a GitHub REST 404. The body must be exactly one JSON
# object in the basic-error shape: message "Not Found" and a docs.github.com
# documentation_url. A status field, when present, must be the string "404".
# The exact documentation path is deliberately not pinned, and a second JSON
# value is rejected rather than allowing one value to mask another.
require_github_not_found() {
  jq -e -s '
    length == 1 and
    (.[0] as $o |
      (($o | type) == "object") and
      ($o.message == "Not Found") and
      (($o.documentation_url | type) == "string") and
      ($o.documentation_url | startswith("https://docs.github.com/")) and
      (($o | has("status") | not) or ($o.status == "404"))
    )
  ' "$latest_body_file" >/dev/null ||
    fail "the latest release returned 404 without a complete GitHub Not Found error body"
}

# A complete 404 permits only published, non-prerelease test tags in the
# list. Drafts and prereleases are excluded by the caller. This reuses
# parse_tag's channel contract and is intentionally not applied to draft
# creation or ordinary pagination, which may accept unrelated tags.
require_404_test_tag() {
  local tag="$1"
  case "$tag" in
    test-v*) parse_tag "$tag" test >/dev/null ;;
    v*)
      parse_tag "$tag" stable >/dev/null
      fail "the latest release returned 404 but the release list contains a published formal release: $tag"
      ;;
    *)
      fail "the latest release returned 404 but the release list contains a published non-test release: $tag"
      ;;
  esac
}

# Output true/false for a stable release. A 404 is valid only with a complete
# GitHub Not Found body and a list whose published non-prerelease objects are
# all legal test tags; drafts and prereleases are excluded. Unknown public
# tags are not test tags and fail closed. A 200 tag must be a valid v/test-v
# tag and must agree with the same list. Any higher published stable version
# prevents an old draft from taking latest.
latest_decision() {
  local version="$1" latest_tag latest_id latest_release list_matches list_count
  local published_version higher published_tags tag
  latest_exchange
  case "$latest_status" in
    404)
      # A complete GitHub 404 is only accepted when the list has no public
      # non-prerelease object, or all such objects are legal test tags.
      # Drafts and prereleases are excluded by existing semantics; unknown
      # public tags are not silently treated as test.
      require_github_not_found
      published_tags=$(printf '%s\n' "$all_releases" | jq -r '
        .[] | select(.draft == false and .prerelease == false) | .tag_name
      ') || fail "cannot classify published releases for a 404 latest"
      while IFS= read -r tag; do
        [ -n "$tag" ] || continue
        require_404_test_tag "$tag"
      done <<EOF
$published_tags
EOF
      printf 'true\n'
      return 0
      ;;
    200) ;;
    *)
      fail "cannot read the latest release: HTTP $latest_status"
      ;;
  esac
  printf '%s\n' "$latest_body" | jq -e '
    type == "object" and
    ((.tag_name | type) == "string") and ((.tag_name | length) > 0) and
    ((.id | type) == "number") and (.id > 0) and
    (.draft == false) and (.prerelease == false)
  ' >/dev/null || fail "the latest release response is malformed"
  latest_tag=$(printf '%s\n' "$latest_body" | jq -r '.tag_name')
  latest_id=$(printf '%s\n' "$latest_body" | jq -r '.id')
  case "$latest_tag" in
    v*) parse_tag "$latest_tag" stable >/dev/null ;;
    test-v*) parse_tag "$latest_tag" test >/dev/null ;;
    *)
      fail "latest release tag $latest_tag is neither v<major.minor.patch> nor test-v<major.minor.patch>"
      ;;
  esac
  list_matches=$(printf '%s\n' "$all_releases" | jq -c --arg tag "$latest_tag" \
    '[.[] | select(.tag_name == $tag)]') ||
    fail "cannot match latest tag $latest_tag in the release list"
  list_count=$(printf '%s\n' "$list_matches" | jq -r 'length') ||
    fail "cannot count latest tag $latest_tag"
  [ "$list_count" = "1" ] ||
    fail "the latest tag $latest_tag is missing or duplicated in the release list"
  latest_release=$(printf '%s\n' "$list_matches" | jq -c '.[0]') ||
    fail "cannot read latest release $latest_tag"
  [ "$(printf '%s\n' "$latest_release" | jq -r '.id')" = "$latest_id" ] ||
    fail "the latest release id $latest_id does not match the release list"
  [ "$(printf '%s\n' "$latest_release" | jq -r '.draft')" = "false" ] ||
    fail "the latest tag $latest_tag is a draft in the release list"
  [ "$(printf '%s\n' "$latest_release" | jq -r '.prerelease')" = "false" ] ||
    fail "the latest tag $latest_tag is a prerelease in the release list"
  higher=false
  published_versions=$(printf '%s\n' "$all_releases" | jq -r '
        .[] | select(.draft == false and .prerelease == false) | .tag_name |
        select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+$")) | sub("^v"; "")
      ') || fail "cannot inspect published stable versions"
  # Versions are validated major.minor.patch, so word splitting is safe here.
  for published_version in $published_versions; do
    if [ "$published_version" != "$version" ] &&
       [ "$(printf '%s\n%s\n' "$version" "$published_version" | sort -V | tail -n 1)" = "$published_version" ]; then
      higher=true
      break
    fi
  done
  if [ "$higher" = "true" ]; then
    printf 'false\n'
  else
    printf 'true\n'
  fi
}

# Output {"release":{...},"make_latest":true|false}. The release object and
# the published-state facts come from the same validated list.
publish_state() {
  local tag="$1" channel="$2" version state release draft make_latest
  version=$(parse_tag "$tag" "$channel") || exit 1
  load_releases
  state=$(target_state "$tag") || exit 1
  [ "$(printf '%s\n' "$state" | jq -r '.state')" = "release" ] ||
    fail "there is no release for tag $tag"
  release=$(printf '%s\n' "$state" | jq -c '.release') ||
    fail "cannot read release $tag"
  draft=$(printf '%s\n' "$release" | jq -r '.draft') ||
    fail "cannot read the draft state for tag $tag"
  [ "$draft" = "true" ] || fail "release $tag is already published"
  case "$channel" in
    test)
      make_latest=false
      ;;
    stable)
      make_latest=$(latest_decision "$version") || exit 1
      ;;
    *)
      fail "unknown channel: $channel"
      ;;
  esac
  [ "$make_latest" = "true" ] || [ "$make_latest" = "false" ] ||
    fail "internal error: make_latest is not a boolean"
  printf '{"release":%s,"make_latest":%s}\n' "$release" "$make_latest"
}

case "$command" in
  tag)
    [ $# -eq 1 ] || fail "usage: release-state.sh tag <tag>"
    load_releases
    target_state "$1"
    ;;
  draft)
    [ $# -eq 1 ] || fail "usage: release-state.sh draft <tag>"
    load_releases
    require_draft "$1"
    ;;
  publish)
    [ $# -eq 2 ] || fail "usage: release-state.sh publish <tag> <test|stable>"
    publish_state "$1" "$2"
    ;;
  *)
    fail "usage: release-state.sh <tag <tag>|draft <tag>|publish <tag> <test|stable>>"
    ;;
esac
