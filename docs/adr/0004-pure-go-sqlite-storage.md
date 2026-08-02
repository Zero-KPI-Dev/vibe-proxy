# ADR-0004: Use pure-Go SQLite for the embedded store

## Status

Accepted

## Decision date

2026-07-29

## Recorded date

2026-08-02 (retrospective)

## Context

vibe-proxy uses an embedded SQLite database for request history, transformations, metrics, and retention. The original `github.com/mattn/go-sqlite3` driver required CGO and a C compiler, which complicated Windows development, Linux container builds, and cross-compilation for Windows, macOS, and Linux on AMD64 and ARM64.

The project needed a CGO-free release path without changing the on-disk SQLite format, schema, Store API, or existing user data.

## Decision

Use `modernc.org/sqlite` as the SQLite driver for the embedded Store and keep default, container, test, and release builds compatible with `CGO_ENABLED=0`.

- Continue using the existing SQLite database file and schema. Do not export, rebuild, or silently relocate existing data as part of the driver change.
- Preserve the public Store entry point and the behavior of request logs, transformation metadata, metrics, and retention.
- Open SQLite through a platform-safe `file:` URI so Windows drive letters, UNC paths, spaces, `?`, and `#` are encoded correctly.
- Retain WAL mode and a 5000 ms busy timeout, and verify their runtime values instead of assuming driver defaults match.
- Validate compatibility with a database fixture created by the former driver, including committed data recoverable from a WAL file.
- Pin the selected driver and its transitive versions through `go.mod` and `go.sum`; upgrade it through normal dependency review rather than treating the original version as permanently fixed.
- Keep the six target OS/architecture builds free of a C toolchain requirement.

This decision changes the driver implementation, not the choice of SQLite as the embedded data model.

## Consequences

### Positive

- Windows, macOS, and Linux binaries can be cross-compiled without platform C compilers.
- Container and release builds use the same pure-Go dependency path as ordinary tests.
- Existing SQLite files remain directly readable and writable.
- The project avoids maintaining separate CGO and non-CGO production variants.
- Windows path behavior and database connection settings become explicit and covered by tests.

### Negative

- The pure-Go driver has a larger dependency graph and can increase compilation time and binary size.
- Driver differences can affect DSN parsing, locking, time values, WAL recovery, and performance.
- Dependency upgrades must remain compatible with the project's minimum Go version and the pinned `modernc.org/libc` dependency selected by the driver.
- Performance characteristics may differ from the C SQLite implementation and require measurement under representative load.

### Neutral

- The database remains SQLite, so existing backup, file-permission, and retention practices continue to apply.
- The decision does not add encryption, remote storage, replication, or a pluggable database abstraction.

## Alternatives considered

### Retain `mattn/go-sqlite3` and use native build toolchains

This was the documented rollback option. It preserves the mature C driver but requires a working C toolchain for local Windows builds and platform-specific native release jobs, increasing packaging and contributor complexity.

### Maintain separate CGO and pure-Go build variants

This was rejected during retrospective review because two production storage implementations would double compatibility testing, make bug reports build-dependent, and weaken the single cross-platform release contract.

### Replace SQLite with another embedded database

This was rejected during retrospective review because it would change the persisted format and operational model, require a data migration, and provide no benefit necessary to solve the CGO constraint.

### Move storage to a remote database

This was rejected because vibe-proxy is designed to work as a self-contained local gateway. A remote service would add deployment, credential, availability, and migration obligations unrelated to the build problem.

## Security and operational considerations

- The migration preserves the existing unencrypted SQLite file. Operating-system file permissions and application-data directory protection remain part of the trust boundary.
- Opening a database must complete connection verification and schema migration before success; partial failures close the created connection and leave existing data intact.
- Real pre-migration database fixtures and WAL recovery tests protect against silent data loss that a SQL-dump-only test would miss.
- Backups should include the database and active WAL state or use SQLite-aware backup procedures.
- CI verifies CGO-free tests and builds so a later dependency does not accidentally reintroduce a native toolchain requirement.
- A future driver regression in compatibility, locking, or resource cost can justify a new superseding ADR; it must not silently swap the on-disk format.

## References

- [Pure-Go SQLite Migration Design](../superpowers/specs/2026-07-29-go-lifecycle-pure-go-sqlite-design.md)
- [Cross-Platform Release Design](../superpowers/specs/2026-07-30-cross-platform-release-design.md)
- [`d35ffd1` — design the pure-Go SQLite migration](https://github.com/a448582655/vibe-proxy/commit/d35ffd1)
- [`9a4caf7` — migrate SQLite storage to pure Go](https://github.com/a448582655/vibe-proxy/commit/9a4caf7)

