ALTER TABLE resource_dimensions
    ADD CONSTRAINT resource_dimensions_unit_canonical
        CHECK (unit ~ '^[a-z][a-z0-9_]{0,62}$') NOT VALID,
    ADD CONSTRAINT resource_dimensions_class_aggregation
        CHECK (
            (class = 'CONSUMABLE' AND aggregation = 'SUM') OR
            (class = 'STRUCTURAL' AND aggregation IN ('MAX', 'MIN')) OR
            (class = 'DEADLINE' AND aggregation = 'ABSOLUTE')
        ) NOT VALID,
    ADD CONSTRAINT resource_dimensions_deadline_representation
        CHECK (
            class <> 'DEADLINE' OR
            (unit = 'unix_microseconds_utc' AND scale = 0)
        ) NOT VALID;

ALTER TABLE resource_dimensions VALIDATE CONSTRAINT resource_dimensions_unit_canonical;
ALTER TABLE resource_dimensions VALIDATE CONSTRAINT resource_dimensions_class_aggregation;
ALTER TABLE resource_dimensions VALIDATE CONSTRAINT resource_dimensions_deadline_representation;

---- create above / drop below ----

ALTER TABLE resource_dimensions
    DROP CONSTRAINT resource_dimensions_deadline_representation,
    DROP CONSTRAINT resource_dimensions_class_aggregation,
    DROP CONSTRAINT resource_dimensions_unit_canonical;
