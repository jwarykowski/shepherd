#!/usr/bin/env bash
# Release on a version bump: when herdr-plugin.toml's version has no tag yet,
# tag the pushed commit v<version> and hand over to GoReleaser (in
# .github/workflows/release.yml), which builds the release and its formula.
#
#   master  stable versions (1.2.3)        → Formula/shepherd.rb
#   pro     pre-releases (1.2.3-alpha.1)   → Formula/shepherd-alpha.rb
#
# Anything else (a stable version on pro, a pre-release on master) is left
# alone. Writes tag, version and formula to $GITHUB_OUTPUT when it tags, and
# nothing when there's nothing to release. DRY_RUN=1 prints what it would do.
set -euo pipefail

branch=${1:?usage: release.sh <branch>}
repo=jwarykowski/shepherd

version=$(sed -n 's/^version = "\(.*\)"/\1/p' herdr-plugin.toml)
[ -n "$version" ] || { echo "no version in herdr-plugin.toml" >&2; exit 1; }
tag="v$version"

case "$branch:$version" in
master:*-*) echo "master carries a pre-release version ($version); not releasing"; exit 0 ;;
master:*) formula=shepherd ;;
pro:*-*) formula=shepherd-alpha ;;
pro:*) echo "pro carries a stable version ($version); release it from master"; exit 0 ;;
*) echo "no releases from $branch"; exit 0 ;;
esac

if git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null 2>&1; then
	echo "$tag already exists; nothing to release"
	exit 0
fi

echo "releasing $tag from $branch as $formula"
if [ -n "${DRY_RUN:-}" ]; then
	echo "would tag $tag, push it and wait for it on GitHub"
	exit 0
fi

# the runner has no git identity, and an annotated tag needs one
git -c user.name="Jonathan Warykowski" -c user.email="jonathan@warykowski.io" \
	-c tag.gpgSign=false tag -a "$tag" -m "$tag"
git push origin "$tag"

# origin is Forgejo; the release goes on GitHub's copy, which gets the tag from
# the push mirror. Publishing first would make GitHub create its own tag.
for i in $(seq 1 30); do
	git ls-remote --exit-code --tags "https://github.com/$repo.git" "refs/tags/$tag" >/dev/null 2>&1 && break
	[ "$i" = 30 ] && { echo "$tag didn't reach GitHub in 5 minutes; check the push mirror" >&2; exit 1; }
	sleep 10
done

{
	echo "tag=$tag"
	echo "version=$version"
	echo "formula=$formula"
} >>"${GITHUB_OUTPUT:-/dev/stdout}"
