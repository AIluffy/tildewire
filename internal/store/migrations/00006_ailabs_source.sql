-- +goose Up
INSERT OR IGNORE INTO sources (id, name, status)
VALUES ('ailabs', 'AI Labs', 'UNKNOWN');

-- +goose Down
DELETE FROM sources WHERE id = 'ailabs';
