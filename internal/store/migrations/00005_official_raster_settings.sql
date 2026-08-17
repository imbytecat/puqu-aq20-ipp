-- +goose Up
CREATE TABLE legacy_profile_printer_settings (
    profile_id INTEGER PRIMARY KEY,
    paper_type INTEGER NOT NULL,
    darkness INTEGER NOT NULL,
    speed INTEGER NOT NULL
) STRICT;
INSERT INTO legacy_profile_printer_settings (profile_id, paper_type, darkness, speed)
SELECT id, paper_type, darkness, speed FROM label_profiles;
ALTER TABLE label_profiles ADD COLUMN halftone_method INTEGER NOT NULL DEFAULT 0 CHECK (halftone_method BETWEEN 0 AND 3);
ALTER TABLE label_profiles ADD COLUMN brightness INTEGER NOT NULL DEFAULT 0 CHECK (brightness BETWEEN -10 AND 10);
ALTER TABLE label_profiles DROP COLUMN paper_type;
ALTER TABLE label_profiles DROP COLUMN darkness;
ALTER TABLE label_profiles DROP COLUMN speed;

-- +goose Down
ALTER TABLE label_profiles ADD COLUMN paper_type INTEGER NOT NULL DEFAULT 2 CHECK (paper_type BETWEEN 1 AND 3);
ALTER TABLE label_profiles ADD COLUMN darkness INTEGER NOT NULL DEFAULT 8 CHECK (darkness BETWEEN 0 AND 11);
ALTER TABLE label_profiles ADD COLUMN speed INTEGER NOT NULL DEFAULT 3 CHECK (speed BETWEEN 0 AND 5);
UPDATE label_profiles
SET paper_type = (SELECT paper_type FROM legacy_profile_printer_settings WHERE profile_id = label_profiles.id),
    darkness = (SELECT darkness FROM legacy_profile_printer_settings WHERE profile_id = label_profiles.id),
    speed = (SELECT speed FROM legacy_profile_printer_settings WHERE profile_id = label_profiles.id)
WHERE id IN (SELECT profile_id FROM legacy_profile_printer_settings);
ALTER TABLE label_profiles DROP COLUMN halftone_method;
ALTER TABLE label_profiles DROP COLUMN brightness;
DROP TABLE legacy_profile_printer_settings;
