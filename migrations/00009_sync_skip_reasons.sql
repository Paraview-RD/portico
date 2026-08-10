-- +goose Up

-- Why entries were skipped, not merely how many.
--
-- A run reporting "6 skipped" and nothing else sends an operator to the
-- documentation, which says a skip is most often a username collision — and
-- when it is not, that sentence is a wrong lead. It cost the walkthrough
-- several rounds to find that every account was being refused for a phone
-- number formatted with spaces, because nothing anywhere recorded the
-- reason.
--
-- One column rather than a table of entries. What an operator needs is the
-- shape of the problem — "5 × invalid phone number, 1 × username already
-- taken" with an example of each — not a row per entry, which for a
-- misconfigured source would be a row per account in the directory.
ALTER TABLE ldap_sync_runs
    ADD COLUMN skipped_detail TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE ldap_sync_runs DROP COLUMN skipped_detail;
