CREATE TRIGGER resource_reservation_items_immutable
BEFORE UPDATE OR DELETE ON resource_reservation_items
FOR EACH ROW EXECUTE FUNCTION reject_agent_artifact_mutation();

---- create above / drop below ----

DROP TRIGGER resource_reservation_items_immutable ON resource_reservation_items;
