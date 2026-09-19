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

This is repository regression evidence. Deployed failure-window replay and
production capacity/resilience qualification remain separate gates.
