-- +goose Up

-- Which protocol a provider actually speaks.
--
-- Until now there was one answer and it did not need recording: an issuer
-- with a discovery document, which is all of OpenID Connect's negotiation
-- done for you. WeChat and DingTalk have neither. Each is OAuth 2 with its
-- own token endpoint, its own userinfo shape, its own name for the subject,
-- and no metadata to fetch — so the code has to know which one it is talking
-- to before it says anything, and a column is the only honest place for
-- that.
--
-- Not inferred from the issuer string. That would work, and it would mean a
-- typo in a URL silently changing which protocol is spoken.
ALTER TABLE external_identity_providers
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'OIDC'
        CHECK (kind IN ('OIDC', 'WECHAT', 'DINGTALK'));

COMMENT ON COLUMN external_identity_providers.kind IS
    'OIDC for anything with a discovery document; WECHAT and DINGTALK for the two that have none and need an adapter each.';

-- The issuer column keeps its meaning for all three.
--
-- For a vendor it is not discovered, it is a constant — https://open.weixin.qq.com
-- and https://login.dingtalk.com — and it is stored rather than implied for
-- two reasons. The unique constraint on (tenant_id, issuer) then still says
-- "one WeChat per tenant", which is the rule anybody would expect. And
-- identity here is the pair (issuer, subject) everywhere else in this
-- schema; making one kind of provider an exception to that would be an
-- exception every later query has to remember.

-- +goose Down
ALTER TABLE external_identity_providers DROP COLUMN kind;
