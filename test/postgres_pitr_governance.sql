\set ON_ERROR_STOP on
BEGIN;
UPDATE security_epochs SET security_epoch=1 WHERE singleton;
UPDATE tenant_security_epochs SET policy_epoch=5,revocation_epoch=6 WHERE tenant_id='10000000-0000-4000-8000-000000000001';
UPDATE agent_security_epochs SET revocation_epoch=7 WHERE tenant_id='10000000-0000-4000-8000-000000000001' AND agent_id='10000000-0000-4000-8000-000000000003';
UPDATE resource_balances SET available_value=90,direct_consumed_value=10,state_version=2,updated_at=now() WHERE tenant_id='10000000-0000-4000-8000-000000000001' AND envelope_id='10000000-0000-4000-8000-000000000007' AND dimension_id='10000000-0000-4000-8000-000000000006';
INSERT INTO audit_events(id,tenant_id,principal_id,action,resource_type,resource_id,outcome,event_hash,occurred_at) VALUES ('10000000-0000-4000-8000-000000000011','10000000-0000-4000-8000-000000000001','10000000-0000-4000-8000-000000000002','pitr.governance','resource_envelope','10000000-0000-4000-8000-000000000007','SUCCEEDED','sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',now());
INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,occurred_at,available_at) VALUES ('10000000-0000-4000-8000-000000000012','10000000-0000-4000-8000-000000000001','resource_envelope','10000000-0000-4000-8000-000000000007','pitr.governance',1,'{}',now(),now());
COMMIT;
