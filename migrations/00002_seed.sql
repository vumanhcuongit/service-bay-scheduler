-- Seed data from the booking spec §2 (D11). Tests and the demo share it.
-- +goose Up
INSERT INTO dealerships (id, timezone, open_time, close_time) VALUES
    ('DL1', 'Asia/Ho_Chi_Minh', '08:00', '17:00'),
    ('DL2', 'Asia/Ho_Chi_Minh', '08:00', '17:00');

INSERT INTO skills (id) VALUES ('oil'), ('brake'), ('ev'), ('transmission'), ('tire');

INSERT INTO bays (id, dealership_id) VALUES
    ('B1', 'DL1'),
    ('B2', 'DL1'),
    ('B3', 'DL2');

INSERT INTO technicians (id, dealership_id, work_start, work_end) VALUES
    ('T1', 'DL1', '08:00', '17:00'),
    ('T2', 'DL1', '08:00', '13:00'),
    ('T3', 'DL1', '08:00', '17:00'),
    ('T4', 'DL2', '08:00', '17:00');

INSERT INTO technician_skills (technician_id, skill_id) VALUES
    ('T1', 'oil'), ('T1', 'brake'),
    ('T2', 'oil'), ('T2', 'ev'),
    ('T3', 'oil'),
    ('T4', 'tire');

-- TRANSMISSION: no technician has the skill.
INSERT INTO service_types (id, dealership_id, duration_minutes, required_skill) VALUES
    ('OIL',          'DL1', 30,  'oil'),
    ('BRAKE',        'DL1', 60,  'brake'),
    ('EV_CHECK',     'DL1', 45,  'ev'),
    ('TRANSMISSION', 'DL1', 120, 'transmission'),
    ('TIRE',         'DL2', 30,  'tire');

INSERT INTO customers (id) VALUES ('C1');

INSERT INTO vehicles (id, customer_id) VALUES ('V1', 'C1'), ('V2', 'C1'), ('V3', 'C1');

-- +goose Down
DELETE FROM vehicles WHERE id IN ('V1', 'V2', 'V3');
DELETE FROM customers WHERE id = 'C1';
DELETE FROM service_types WHERE id IN ('OIL', 'BRAKE', 'EV_CHECK', 'TRANSMISSION', 'TIRE');
DELETE FROM technician_skills WHERE technician_id IN ('T1', 'T2', 'T3', 'T4');
DELETE FROM technicians WHERE id IN ('T1', 'T2', 'T3', 'T4');
DELETE FROM bays WHERE id IN ('B1', 'B2', 'B3');
DELETE FROM skills WHERE id IN ('oil', 'brake', 'ev', 'transmission', 'tire');
DELETE FROM dealerships WHERE id IN ('DL1', 'DL2');
