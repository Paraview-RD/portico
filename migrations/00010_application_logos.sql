-- +goose Up

-- Uploaded pictures for application tiles.
--
-- The column that names one has existed since 00003, and it takes either an
-- absolute http(s) address or a path on this server. The second form is the
-- one worth having — it keeps the portal working with no outbound network and
-- tells no third party who opened it — but until now it meant a path to a file
-- an operator had put on the filesystem themselves. That is a reasonable ask
-- of somebody running a container and an unreasonable one of somebody
-- registering an application in a web form.
--
-- So this table holds the bytes, and an upload writes a path into the existing
-- logo_uri. Nothing about the three application tables changes, and the
-- validation that decides what may be rendered as a picture does not change
-- either: an uploaded logo takes exactly the form that was already accepted.
--
-- In the database rather than on disk. docs/backup-and-restore.md says the
-- state of this system is in two places — PostgreSQL and one secret — and a
-- directory of blobs would make that document wrong. It would also have to be
-- shared between replicas, and mounted into a container whose selling point is
-- that it is a single static binary. A few dozen images of a few dozen
-- kilobytes is not why anybody reaches for object storage.
CREATE TABLE application_logos (
    id        TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants (id),

    -- What to send back in Content-Type. Derived from the bytes rather than
    -- from what the uploader claimed, which is the only reason it can be
    -- trusted enough to echo — see service.detectLogoFormat.
    content_type TEXT NOT NULL,

    bytes BYTEA NOT NULL,
    -- Stored alongside rather than computed on read: it is the ETag, and
    -- hashing every response to answer a conditional request would defeat the
    -- point of answering one.
    sha256 TEXT NOT NULL,
    byte_size INTEGER NOT NULL,

    -- What the orphan sweep measures. An upload happens before the form that
    -- would reference it is saved, so cancelling the form leaves a row nobody
    -- points at, and replacing a logo leaves the one it replaced. Neither is
    -- worth a foreign key in the other direction — a picture may be reused by
    -- several applications, and reference counting across three tables would
    -- be more machinery than the problem deserves.
    created_at TIMESTAMPTZ NOT NULL
);

-- The sweep's access pattern: everything old enough to consider, per tenant.
CREATE INDEX idx_application_logos_created ON application_logos (tenant_id, created_at);

COMMENT ON TABLE application_logos IS
    'Uploaded pictures for application tiles. Referenced by path from the logo_uri columns.';

-- +goose Down
DROP TABLE application_logos;
