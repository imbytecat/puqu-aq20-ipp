-- +goose Up
ALTER TABLE ble_devices RENAME TO devices;
ALTER TABLE devices ADD COLUMN transport TEXT NOT NULL DEFAULT 'ble' CHECK (transport IN ('ble', 'usb'));
CREATE TABLE legacy_ble_printer_assignments (
    printer_id INTEGER PRIMARY KEY,
    device_id INTEGER NOT NULL
) STRICT;
INSERT INTO legacy_ble_printer_assignments (printer_id, device_id)
SELECT id, device_id FROM printers WHERE device_id IS NOT NULL;
UPDATE printers SET device_id = NULL WHERE device_id IS NOT NULL;

-- +goose Down
UPDATE printers SET device_id = NULL
WHERE device_id IN (SELECT id FROM devices WHERE transport = 'usb');
UPDATE printers
SET device_id = (SELECT device_id FROM legacy_ble_printer_assignments WHERE printer_id = printers.id)
WHERE id IN (SELECT printer_id FROM legacy_ble_printer_assignments);
DELETE FROM devices WHERE transport = 'usb';
DROP TABLE legacy_ble_printer_assignments;
ALTER TABLE devices DROP COLUMN transport;
ALTER TABLE devices RENAME TO ble_devices;
