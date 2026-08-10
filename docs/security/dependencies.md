# Dependency and Build-Tool Policy

This policy applies to runtime, test, generator, scanner, and build dependencies.
It complements `docs/supported-versions.md` and the supply-chain controls in the
threat model.

## Selection and review

- Prefer the Go standard library and small interfaces at infrastructure edges.
- A new direct dependency requires review of ownership, maintenance, release
  provenance, transitive footprint, license, vulnerabilities, privileged
  behavior, and replacement/rollback cost.
- Runtime dependencies belong behind the `internal/` package boundaries; domain
  and application packages do not acquire infrastructure dependencies merely for
  convenience.
- Module versions must be exact. Branch names, unversioned revisions, `replace`
  directives, vendored edits, unresolved module errors, and retracted versions
  fail the dependency check.
- Pseudo-versions require a time-bounded exception when no tagged fix exists.

## Source policy

`dependency-policy.json` is the machine-readable allowlist of reviewed module
path prefixes. A new source host is denied until its trust, availability, and
ownership/incident model are reviewed. An allowlisted host does not make every
module from that host acceptable; ordinary dependency review still applies.

Exceptions specify an exact module path and version, owner, reason, approval
reference, and ISO-8601 expiry date. Expired, malformed, wildcard, or
version-mismatched exceptions fail closed. The initial exception list is empty.

## License policy

The shipped service may use permissive, notice, and unencumbered dependencies.
Unknown, forbidden, restricted, and reciprocal classifications fail the check.
Test and tool dependencies are included because they execute in trusted
development or CI environments.

Dual-licensed dependencies must select an acceptable license. An exception
requires legal/security approval, exact module/version, required notice or
source-offer handling, an owner, and an expiry. Generated license reports are
evidence artifacts and are not committed unless required for a release.

## Vulnerability policy

`govulncheck` scans application and test call graphs with the pinned Go toolchain
and public Go vulnerability database. Any reachable vulnerability fails. Fix
standard-library findings by advancing the Go patch pin; fix module findings by
upgrading, removing, or proving the vulnerable code unreachable with a rescan.

There is no permanent ignore list. A false positive or temporarily unfixable
finding requires a time-bounded security exception containing the advisory ID,
affected symbols/exposure, compensating controls, owner, expiry, and removal
plan. Critical or high exploitable findings block a release candidate.

## Reproducible tools

Third-party Go tools live in the nested `tools` module so they never enter the
service dependency graph. `tools/go.mod` pins the toolchain and exact tools;
`tools/go.sum` authenticates the full graph. Use these tools through `go tool`
instead of `go install ...@latest`.

From the repository root, the ENG-002 checks are:

```sh
(cd tools && go run ./cmd/dependencycheck -module-dir .. -policy ../dependency-policy.json)
go tool -modfile=tools/go.mod govulncheck -test ./...
go tool -modfile=tools/go.mod go-licenses check --include_tests \
  --ignore github.com/bdobrica/ThinkPixelAG \
  --disallowed_types=forbidden,restricted,reciprocal,unknown ./...
```

ENG-008 will expose these operations through Make targets; ENG-010 will run them
in CI. Network or vulnerability-database errors are failures, not clean results.

## Update and exception workflow

1. Change one logical dependency/tool group and retain the old version for
   rollback.
2. Review release notes, provenance, licenses, advisories, and graph changes
   using `go mod graph` and `go mod why`.
3. Run formatting, unit/race, dependency, license, vulnerability, and relevant
   integration/contract checks.
4. Record why, old/new versions, verification, operational/security effects,
   and any expiring exception in the commit or an ADR.
5. Remove unused modules with `go mod tidy`; never suppress checksum or proxy
   verification to make an update pass.

No exceptions are approved as of 2026-08-10.
