-- +goose Up
ALTER TABLE session_projection ADD COLUMN pr_number INTEGER;
ALTER TABLE session_projection ADD COLUMN pr_state TEXT;
ALTER TABLE session_projection ADD COLUMN pr_ci_state TEXT;
ALTER TABLE session_projection ADD COLUMN pr_updated_at DATETIME;

-- +goose Down
ALTER TABLE session_projection DROP COLUMN pr_updated_at;
ALTER TABLE session_projection DROP COLUMN pr_ci_state;
ALTER TABLE session_projection DROP COLUMN pr_state;
ALTER TABLE session_projection DROP COLUMN pr_number;
