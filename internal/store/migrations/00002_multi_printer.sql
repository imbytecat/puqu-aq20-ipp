-- +goose Up
CREATE TEMP TABLE legacy_defaults AS
SELECT a.ipp_name, a.printer_uuid, a.advertise, a.airprint, a.updated_at,
       (SELECT id FROM ble_devices WHERE selected = 1 LIMIT 1) AS device_id,
       (SELECT id FROM label_profiles WHERE active = 1 LIMIT 1) AS profile_id
FROM app_settings a WHERE a.id = 1;

ALTER TABLE ble_devices RENAME TO ble_devices_old;
CREATE TABLE ble_devices (
    id INTEGER PRIMARY KEY,
    native_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    address TEXT NOT NULL,
    write_uuid TEXT NOT NULL,
    notify_uuid TEXT,
    last_seen_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;
INSERT INTO ble_devices (id, native_id, name, address, write_uuid, notify_uuid, last_seen_at, updated_at)
SELECT id, native_id, name, address, write_uuid, notify_uuid, last_seen_at, updated_at FROM ble_devices_old;
DROP TABLE ble_devices_old;

ALTER TABLE label_profiles RENAME TO label_profiles_old;
CREATE TABLE label_profiles (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    width_um INTEGER NOT NULL CHECK (width_um > 0),
    height_um INTEGER NOT NULL CHECK (height_um > 0),
    gap_um INTEGER NOT NULL CHECK (gap_um >= 0),
    paper_type INTEGER NOT NULL CHECK (paper_type BETWEEN 1 AND 3),
    darkness INTEGER NOT NULL CHECK (darkness BETWEEN 0 AND 11),
    speed INTEGER NOT NULL CHECK (speed BETWEEN 0 AND 5),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;
INSERT INTO label_profiles (id, name, width_um, height_um, gap_um, paper_type, darkness, speed, created_at, updated_at)
SELECT id, name, width_um, height_um, gap_um, paper_type, darkness, speed, created_at, updated_at FROM label_profiles_old;
DROP TABLE label_profiles_old;

CREATE TABLE printers (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    uuid TEXT NOT NULL UNIQUE,
    driver TEXT NOT NULL,
    device_id INTEGER UNIQUE REFERENCES ble_devices(id) ON DELETE SET NULL,
    profile_id INTEGER NOT NULL REFERENCES label_profiles(id) ON DELETE RESTRICT,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    advertise INTEGER NOT NULL CHECK (advertise IN (0, 1)),
    airprint INTEGER NOT NULL CHECK (airprint IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;
INSERT INTO printers (
    name, slug, uuid, driver, device_id, profile_id, enabled, advertise, airprint, created_at, updated_at
)
SELECT ipp_name, 'puqu-aq20',
       CASE WHEN printer_uuid <> '' THEN printer_uuid ELSE
           lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' ||
           substr(lower(hex(randomblob(2))), 2) || '-a' || substr(lower(hex(randomblob(2))), 2) || '-' ||
           lower(hex(randomblob(6)))
       END,
       'puqu-aq20', device_id, profile_id, 1, advertise, airprint, updated_at, updated_at
FROM legacy_defaults;

ALTER TABLE print_jobs RENAME TO print_jobs_old;
CREATE TABLE print_jobs (
    id INTEGER PRIMARY KEY,
    printer_id INTEGER NOT NULL REFERENCES printers(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    user_name TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'processing', 'completed', 'canceled', 'aborted')),
    document_format TEXT NOT NULL,
    copies INTEGER NOT NULL CHECK (copies > 0),
    bytes INTEGER NOT NULL CHECK (bytes >= 0),
    error TEXT,
    created_at INTEGER NOT NULL,
    started_at INTEGER,
    completed_at INTEGER
) STRICT;
INSERT INTO print_jobs (
    id, printer_id, name, user_name, state, document_format, copies, bytes, error, created_at, started_at, completed_at
)
SELECT id, (SELECT id FROM printers LIMIT 1), name, user_name, state, document_format, copies, bytes, error, created_at, started_at, completed_at
FROM print_jobs_old;
DROP TABLE print_jobs_old;
CREATE INDEX print_jobs_created_at ON print_jobs(created_at DESC);
CREATE INDEX print_jobs_printer_state ON print_jobs(printer_id, state, id);

DROP TABLE app_settings;
DROP TABLE legacy_defaults;

-- +goose Down
CREATE TEMP TABLE current_default AS
SELECT (SELECT device_id FROM printers ORDER BY id LIMIT 1) AS device_id,
       (SELECT profile_id FROM printers ORDER BY id LIMIT 1) AS profile_id;

CREATE TABLE app_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    ipp_name TEXT NOT NULL,
    printer_uuid TEXT NOT NULL,
    ipp_listen TEXT NOT NULL,
    admin_listen TEXT NOT NULL,
    advertise INTEGER NOT NULL CHECK (advertise IN (0, 1)),
    airprint INTEGER NOT NULL CHECK (airprint IN (0, 1)),
    updated_at INTEGER NOT NULL
) STRICT;
INSERT INTO app_settings (
    id, ipp_name, printer_uuid, ipp_listen, admin_listen, advertise, airprint, updated_at
)
SELECT 1,
       COALESCE((SELECT name FROM printers ORDER BY id LIMIT 1), 'PUQU AQ20'),
       COALESCE((SELECT uuid FROM printers ORDER BY id LIMIT 1), ''),
       ':8631', '127.0.0.1:8080',
       COALESCE((SELECT advertise FROM printers ORDER BY id LIMIT 1), 1),
       COALESCE((SELECT airprint FROM printers ORDER BY id LIMIT 1), 0),
       COALESCE((SELECT updated_at FROM printers ORDER BY id LIMIT 1), 0);

ALTER TABLE print_jobs RENAME TO print_jobs_new;
CREATE TABLE print_jobs (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    user_name TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'processing', 'completed', 'canceled', 'aborted')),
    document_format TEXT NOT NULL,
    copies INTEGER NOT NULL CHECK (copies > 0),
    bytes INTEGER NOT NULL CHECK (bytes >= 0),
    error TEXT,
    created_at INTEGER NOT NULL,
    started_at INTEGER,
    completed_at INTEGER
) STRICT;
INSERT INTO print_jobs (
    id, name, user_name, state, document_format, copies, bytes, error, created_at, started_at, completed_at
)
SELECT id, name, user_name, state, document_format, copies, bytes, error, created_at, started_at, completed_at
FROM print_jobs_new;
DROP TABLE print_jobs_new;
CREATE INDEX print_jobs_created_at ON print_jobs(created_at DESC);
CREATE INDEX print_jobs_state ON print_jobs(state);
DROP TABLE printers;

ALTER TABLE ble_devices RENAME TO ble_devices_new;
CREATE TABLE ble_devices (
    id INTEGER PRIMARY KEY,
    native_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    address TEXT NOT NULL,
    write_uuid TEXT NOT NULL,
    notify_uuid TEXT,
    selected INTEGER NOT NULL CHECK (selected IN (0, 1)),
    last_seen_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;
INSERT INTO ble_devices (id, native_id, name, address, write_uuid, notify_uuid, selected, last_seen_at, updated_at)
SELECT id, native_id, name, address, write_uuid, notify_uuid,
       CASE WHEN id = (SELECT device_id FROM current_default) THEN 1 ELSE 0 END,
       last_seen_at, updated_at
FROM ble_devices_new;
DROP TABLE ble_devices_new;
CREATE UNIQUE INDEX one_selected_device ON ble_devices(selected) WHERE selected = 1;

ALTER TABLE label_profiles RENAME TO label_profiles_new;
CREATE TABLE label_profiles (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    width_um INTEGER NOT NULL CHECK (width_um > 0),
    height_um INTEGER NOT NULL CHECK (height_um > 0),
    gap_um INTEGER NOT NULL CHECK (gap_um >= 0),
    paper_type INTEGER NOT NULL CHECK (paper_type BETWEEN 1 AND 3),
    darkness INTEGER NOT NULL CHECK (darkness BETWEEN 0 AND 11),
    speed INTEGER NOT NULL CHECK (speed BETWEEN 0 AND 5),
    active INTEGER NOT NULL CHECK (active IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;
INSERT INTO label_profiles (id, name, width_um, height_um, gap_um, paper_type, darkness, speed, active, created_at, updated_at)
SELECT id, name, width_um, height_um, gap_um, paper_type, darkness, speed,
       CASE WHEN id = (SELECT profile_id FROM current_default) THEN 1 ELSE 0 END,
       created_at, updated_at
FROM label_profiles_new;
DROP TABLE label_profiles_new;
CREATE UNIQUE INDEX one_active_profile ON label_profiles(active) WHERE active = 1;
DROP TABLE current_default;
