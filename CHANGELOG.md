# Changelog

All notable changes to this module are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [1.1.0] - 2026-10-19

### Added

- Command line tool: `go install github.com/yumaheymans/carejobs-go/cmd/carejobs@latest`.
  Commands `search`, `pay`, `market`, `state`, `employers`, `employer` and `tools`, with a
  readable default and `--json` for the raw data, and the same options and short kind names
  (`rn`, `cna`, `np`, ...) as the npm and Python command line tools. The default timeout is
  60 seconds.
- Release binaries for Windows, macOS and Linux (`amd64` and `arm64`) with a `checksums.txt`,
  built by `scripts/build-release.sh` and attached to the GitHub release by
  `.github/workflows/release.yml` when a `v*` tag is pushed.

### Changed

- `Version` is now `1.1.0`, so the default User-Agent is `carejobs-go/1.1.0`.

## [1.0.0] - 2026-10-19

### Added

- `Client` with `SearchJobs`, `Pay`, `JobMarket`, `JobsByState`, `Employers` and `Employer`,
  each returning the typed data of the CareJobs.Now MCP tool plus the readable summary as
  `Text`.
- `Tools` and `Call` for the server's tool list and for any tool the server adds later.
- `*Error` with a `Code` of `tool`, `rpc`, `http`, `network`, `timeout`, `canceled` or
  `protocol`, and `errors.Is` support for `context.DeadlineExceeded` and `context.Canceled`.
- `Kind`, `Sort`, `EmployerType` and `Care` with constants taken from the live `tools/list`
  schemas.
- Standard library only. Go 1.24 or newer.

[1.1.0]: https://github.com/yumaheymans/carejobs-go/releases/tag/v1.1.0
[1.0.0]: https://github.com/yumaheymans/carejobs-go/releases/tag/v1.0.0
