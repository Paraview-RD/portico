-- +goose Up

-- When an administrator last gave this account its recovery allowance back.
--
-- Password recovery is capped at a few messages per account per day, because
-- every message spends a sending quota and a sender reputation that all
-- tenants on the deployment share. That cap is silent by design: telling the
-- caller "too many" would confirm that the submitted address has an account
-- here, which is the one thing the endpoint refuses to disclose.
--
-- Silent and permanent are different things, though, and without this column
-- it was both. An account holder reporting "no reset message ever arrives"
-- left an administrator with exactly one move — setting the password by hand
-- — which is the heavier action that Unlock exists so a lockout does not
-- require. This column is the equivalent of the unlock button.
--
-- A timestamp rather than a counter reset, and no rows are deleted. The count
-- is taken over the window starting at the later of "a day ago" and this
-- value, so clearing the allowance moves where counting begins and leaves
-- password_resets exactly as it was. That table is kept for thirty days so
-- that "was a reset link ever issued for this account, and was it used" stays
-- answerable; handing somebody their allowance back is not a reason to stop
-- being able to answer it.
--
-- NULL means never cleared, which is every account that exists today and
-- almost every account that ever will.
ALTER TABLE users ADD COLUMN recovery_quota_cleared_at TIMESTAMPTZ;

COMMENT ON COLUMN users.recovery_quota_cleared_at IS
    'When an administrator last cleared this account''s daily password-recovery allowance. The per-day count is taken from the later of this value and twenty-four hours ago, so clearing moves the start of the window without deleting the password_resets rows that record what was sent. NULL means never cleared.';

-- No index. The column is read only on the recovery path, which has already
-- fetched the row by address, and written only by an administrator acting on
-- one account. Nothing ever searches by it.

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS recovery_quota_cleared_at;
