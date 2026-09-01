# Contributing

Thank you for helping improve `lw`.

## Development setup

Install Go 1.26 or newer and Git, then run:

```sh
go mod download
go test ./...
```

Before submitting a change, run the complete local gate:

```sh
test -z "$(gofmt -l .)"
go test ./...
go test -race ./...
go vet ./...
go mod verify
shellcheck install.sh scripts/generate-third-party-notices.sh
./scripts/generate-third-party-notices.sh
git diff --check
git diff --exit-code -- THIRD_PARTY_NOTICES.md
```

Tests must not contact real issue providers, read real credentials, or modify user repositories. Use
synthetic issue identifiers, titles, organization names, URLs, paths, and API-key-shaped
values in fixtures.

If dependency changes alter the code linked into release binaries, commit the regenerated
`THIRD_PARTY_NOTICES.md`.

## Pull requests

Keep changes focused and explain the user-visible behavior. Add tests for behavior changes.
Do not commit generated binaries, credentials, private issue data, or environment-specific
handoff notes.

Contributions submitted to this repository are licensed under the [MIT License](LICENSE).
