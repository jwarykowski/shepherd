#!/usr/bin/env bash
# Release on a version bump: when herdr-plugin.toml's version has no tag yet,
# tag the pushed commit v<version> and point the Homebrew formula at it.
#
#   master  stable versions (1.2.3)        → Formula/shepherd.rb
#   pro     pre-releases (1.2.3-alpha.1)   → Formula/shepherd-alpha.rb
#
# Anything else (a stable version on pro, a pre-release on master) is left
# alone. Needs GITHUB_TOKEN (to push the tag) and TAP_TOKEN (to open and merge
# the formula pull request on jwarykowski/homebrew-tap). DRY_RUN=1 prints what
# it would do.
set -euo pipefail

branch=${1:?usage: release.sh <branch>}
repo=jwarykowski/shepherd
tap=jwarykowski/homebrew-tap

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

run() {
	if [ -n "${DRY_RUN:-}" ]; then
		echo "would run: $*"
	else
		"$@"
	fi
}

echo "releasing $tag from $branch as $formula"
run git -c tag.gpgSign=false tag -a "$tag" -m "$tag"
run git push origin "$tag"
if [ -n "${DRY_RUN:-}" ]; then
	exit 0
fi

url="https://github.com/$repo/archive/refs/tags/$tag.tar.gz"
sha=""
for _ in 1 2 3 4 5; do # the archive can take a moment to exist
	if sha=$(curl -fsSL "$url" | sha256sum | cut -d' ' -f1) && [ -n "$sha" ]; then
		break
	fi
	sleep 5
done
[ -n "$sha" ] || { echo "couldn't fetch $url" >&2; exit 1; }

work=$(mktemp -d)
GH_TOKEN=$TAP_TOKEN gh repo clone "$tap" "$work/tap" -- --quiet --depth 1
cd "$work/tap"
file="Formula/$formula.rb"
sed -i.bak \
	-e "s|^  url \".*\"|  url \"$url\"|" \
	-e "s|^  sha256 \".*\"|  sha256 \"$sha\"|" \
	-e "s|^  version \".*\"|  version \"$version\"|" \
	-e "s|assert_match \"[0-9][^\"]*\", shell_output(\"#{bin}/shepherd --version\")|assert_match \"$version\", shell_output(\"#{bin}/shepherd --version\")|" \
	"$file"
rm "$file.bak"
git switch -q -c "$formula-$version"
git -c user.name="Jonathan Warykowski" -c user.email="jonathan@warykowski.io" \
	commit -q --no-gpg-sign -am "feat: $formula $version"
git push -q "https://x-access-token:$TAP_TOKEN@github.com/$tap.git" "$formula-$version"
GH_TOKEN=$TAP_TOKEN gh pr create -R "$tap" --base main --head "$formula-$version" \
	--title "feat: $formula $version" --body "Points $formula at $tag ($url, sha256 $sha)."
GH_TOKEN=$TAP_TOKEN gh pr merge -R "$tap" --squash --delete-branch "$formula-$version"
echo "released $tag; brew upgrade $formula picks it up"
