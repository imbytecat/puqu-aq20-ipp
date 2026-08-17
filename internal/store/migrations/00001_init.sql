-- +goose Up
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

CREATE UNIQUE INDEX one_selected_device ON ble_devices(selected) WHERE selected = 1;

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

CREATE UNIQUE INDEX one_active_profile ON label_profiles(active) WHERE active = 1;

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

CREATE INDEX print_jobs_created_at ON print_jobs(created_at DESC);
CREATE INDEX print_jobs_state ON print_jobs(state);

INSERT INTO app_settings (
    id, ipp_name, printer_uuid, ipp_listen, admin_listen, advertise, airprint, updated_at
) VALUES (1, 'PUQU AQ20', '', ':8631', '127.0.0.1:8080', 1, 0, 0);

INSERT INTO label_profiles (
    name, width_um, height_um, gap_um, paper_type, darkness, speed, active, created_at, updated_at
) VALUES ('40 × 30 mm Gap', 40000, 30000, 2000, 2, 8, 3, 1, 0, 0);

-- +goose Down
DROP TABLE print_jobs;
DROP TABLE label_profiles;
DROP TABLE ble_devices;
DROP TABLE app_settings;
