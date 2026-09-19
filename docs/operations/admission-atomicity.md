# Admission replay atomicity

The wired OPS-010 retry addresses the previously reproduced gap between the
Run commit and idempotency response completion. The HTTP admission application
port now requires atomic persistence: PostgreSQL locks the current ownership
record, writes the aggregate and exact sanitized replay response, then commits
through the Transactor at READ COMMITTED isolation. Replaced and completed
owners cannot mutate. Policy evaluation and response encoding precede the
transaction. Public request/response contracts are unchanged.

Focused unit and race tests passed for application, HTTP, PostgreSQL and runtime
packages. The PostgreSQL integration admission suite passed against the retained
`ops_verify` database on 2026-09-19 (17.658 seconds). A scoped trigger rejected
response completion after aggregate insertion: no Run, resolution, envelope,
event, audit or outbox survived. Lease-expiry reacquisition rejected the stale
owner, admitted once, and replayed the response on repeated requests. Completed
ownership could not create another Run.

The patched ARM64 API image from `406db54` is
`sha256:a8a8a3584350a62c8ad0dd52b096ab40bce45072b9356b11cbbe052d89d2d79e`.
On the retained wired cluster, a 20-second completion delay returned 503 after
10.265 seconds with zero new Runs; after lease expiry, retry returned 201 and
created exactly one Run. The persistent database kept its durability settings.
An earlier attempt overlapped flash-heavy regression checkpoints: both requests
returned 500 and neither created a Run. It is retained as a failed attempt, not
counted as successful retry qualification.

Full `make verify` passed after moving the isolated regression database to a
separate 1 GiB RAM-backed volume (PostgreSQL integration 18.437 seconds;
end-to-end tests 11.007 seconds). The initial full gate on shared persistent
storage was stopped during checkpoint stalls. RAM-backed regression results
are not crash-durability evidence. Production capacity/resilience qualification
remains separate from the fixed admission retry window.
