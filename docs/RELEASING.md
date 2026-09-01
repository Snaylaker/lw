# Releasing lw

## One-time repository setup

Before making the repository public:

- Confirm that every contributor and applicable employer has approved the Apache-2.0 release.
- Enable GitHub secret scanning, push protection, Dependabot alerts and private vulnerability reporting.
- Protect `main`; require the `ci` workflow and at least one approving review where practical.
- Restrict tag creation for `v*` tags to maintainers.
- Allow GitHub Actions to create artifact attestations.
- Check that repository topics, description and default branch contain no private organization data.

## Release gate

Run from a clean checkout:

```sh
test -z "$(gofmt -l .)"
go mod verify
go test ./...
go test -race ./...
go vet ./...
shellcheck install.sh scripts/generate-third-party-notices.sh
actionlint
./scripts/generate-third-party-notices.sh
git diff --check
git diff --exit-code
```

Run a local secret scan against both the working tree and all reachable Git history. Never
allowlist a finding until its value and provenance have been reviewed without exposing the
value in logs.

## Publish

Releases use immutable semantic-version tags:

```sh
VERSION=v1.2.3
git tag -s "$VERSION" -m "lw $VERSION"
git push origin "$VERSION"
```

The release workflow verifies the tag, runs the full gate, cross-builds four targets,
packages licenses, creates checksums, publishes provenance attestations, and refuses to
replace an existing release.

After publication, test the documented installer in a disposable environment and verify one
asset independently:

```sh
VERSION=v1.2.3
gh release download "$VERSION" --repo snaylaker/lw
gh attestation verify "lw_${VERSION#v}_linux_amd64.tar.gz" --repo snaylaker/lw
sha256sum --check checksums.txt
```
