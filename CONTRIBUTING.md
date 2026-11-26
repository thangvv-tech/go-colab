# Contributing

## Prerequisites

- Go 1.26+
- Git

## Validation

Run formatting, module verification, unit tests, static analysis, and a CLI build before opening a pull request. CI enforces these checks on supported Go versions.

Live integration validation allocates a real runtime, requires explicit credential setup, and can consume compute units. Coordinate it with a maintainer; do not run it from routine pull-request CI.

Use the `Makefile` targets for local development workflows.

## Pull requests

- Keep library behavior in `colab`; keep terminal and process handling in `cmd/colab`.
- Add unit tests for protocol changes using `httptest` or WebSocket mocks.
- Do not commit credentials, session metadata, OAuth tokens, private SSH keys, or live runtime URLs.
- Keep public API changes documented in `README.md` and `CHANGELOG.md`.
- Use conventional commit subjects: `feat:`, `fix:`, `docs:`, `test:`, `build:`, or `chore:`.

## Releases

Create an annotated or signed `vMAJOR.MINOR.PATCH` tag. GitHub Actions builds release archives from the tag. The Go module and CLI are independently versioned from every Python project.
