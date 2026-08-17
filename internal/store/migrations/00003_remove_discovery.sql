-- +goose Up
ALTER TABLE printers DROP COLUMN advertise;
ALTER TABLE printers DROP COLUMN airprint;

-- +goose Down
ALTER TABLE printers ADD COLUMN advertise INTEGER NOT NULL DEFAULT 0 CHECK (advertise IN (0, 1));
ALTER TABLE printers ADD COLUMN airprint INTEGER NOT NULL DEFAULT 0 CHECK (airprint IN (0, 1));
