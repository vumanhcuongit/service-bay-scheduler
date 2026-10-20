-- The overlap reads (ListBusy, VehicleHasOverlap) use the gist indexes behind the exclusion
-- constraints; no query seeks on (dealership_id, start). EXPLAIN over 105k rows used
-- appointments_vehicle_excl for both.
-- +goose Up
DROP INDEX appointments_dealership_start_idx;

-- +goose Down
CREATE INDEX appointments_dealership_start_idx ON appointments (dealership_id, start);
