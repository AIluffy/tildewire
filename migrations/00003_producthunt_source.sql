-- +goose Up
INSERT OR IGNORE INTO sources (id, name, status)
VALUES ('producthunt', 'Product Hunt', 'UNKNOWN');

-- +goose Down
DELETE FROM sources WHERE id = 'producthunt';
