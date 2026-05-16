-- +goose Up
INSERT OR IGNORE INTO sources (id, name, status)
VALUES ('lobsters', 'Lobsters', 'UNKNOWN');

-- +goose Down
DELETE FROM sources WHERE id = 'lobsters';
