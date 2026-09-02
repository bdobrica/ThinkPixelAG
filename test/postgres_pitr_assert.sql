\set ON_ERROR_STOP on
SELECT (SELECT security_epoch FROM security_epochs WHERE singleton) = :expected_security_epoch AND
       (SELECT policy_epoch FROM tenant_security_epochs WHERE tenant_id='10000000-0000-4000-8000-000000000001') = :expected_policy_epoch AND
       (SELECT revocation_epoch FROM tenant_security_epochs WHERE tenant_id='10000000-0000-4000-8000-000000000001') = :expected_tenant_revocation_epoch AND
       (SELECT revocation_epoch FROM agent_security_epochs WHERE tenant_id='10000000-0000-4000-8000-000000000001' AND agent_id='10000000-0000-4000-8000-000000000003') = :expected_agent_revocation_epoch AS epoch_ok,
       (SELECT count(*) FROM outbox_messages WHERE tenant_id='10000000-0000-4000-8000-000000000001') = :expected_outbox AS outbox_ok,
       (SELECT count(*) FROM audit_events WHERE tenant_id='10000000-0000-4000-8000-000000000001') = :expected_audit AS audit_ok,
       (SELECT direct_consumed_value FROM resource_balances WHERE tenant_id='10000000-0000-4000-8000-000000000001' AND envelope_id='10000000-0000-4000-8000-000000000007' AND dimension_id='10000000-0000-4000-8000-000000000006') = :expected_consumed AS allocation_ok
\gset
\if :epoch_ok
\else
  \echo security epoch is not at recovery target
  \quit 1
\endif
\if :outbox_ok
\else
  \echo outbox is not at recovery target
  \quit 1
\endif
\if :audit_ok
\else
  \echo audit evidence is not at recovery target
  \quit 1
\endif
\if :allocation_ok
\else
  \echo allocation ledger is not at recovery target
  \quit 1
\endif
