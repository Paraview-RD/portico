# Running it in production

What you deploy is one binary and one PostgreSQL. The console, the manual
and the API are the same process — there is no static site to host, no
worker to schedule, and nothing to put in front of it except whatever
terminates your TLS.

This page is the part that is not in [Getting in](access-guide.md): what an
orchestrator needs to know, what happens when you run more than one of
these, and what an upgrade does.

## The three secrets

Everything else has a working default. These do not, or should not.

| | What it costs if you lose it |
|---|---|
| `PORTICO_DB_DSN` | Nothing starts. Unset, the server says so and exits — deliberately, since a database chosen by default would be the wrong database. |
| `PORTICO_JWT_SECRET` | Every session ends. It does not stop a start: unset, a random one is generated **per process**, so sessions die on restart and, with more than one instance, a token minted by one is rejected by the others. |
| `PORTICO_ENCRYPTION_KEY` | Directory bind passwords, webhook headers and external client secrets become unreadable ciphertext. The rows survive; what they hold does not. |

The last two are not in the database, so **a `pg_dump` is not a backup of
this system**. [Backup and restore](backup-and-restore.md) says what else to
copy and what each omission costs when you use the copy.

## Liveness and readiness are different endpoints

```
GET /api/v1/health   → 200 always, no dependencies
GET /api/v1/ready    → 200, or 503 NOT_READY when the database is unreachable
```

Point the **liveness** probe at `/health` and the **readiness** probe at
`/ready`, and not the other way round. The reasoning is in the handler and
is worth repeating here, because getting it backwards is the mistake that
turns one bad afternoon into an outage:

- Liveness asks *is this process broken, should I restart it*. A database
  outage is not fixed by restarting every instance, and doing so turns one
  failing dependency into a restart loop across the fleet at the moment it
  is least able to cope.
- Readiness asks *should I send traffic here*. During that same outage the
  answer is no.

So `/health` stays dependency-free on purpose. If you ever add a dependency
check, `/ready` is where it goes.

The container image has no shell and no `curl`, so its own `HEALTHCHECK`
runs `portico ready` — a subcommand that exists for exactly that reason. It
is available to you too, and takes `--addr`.

## Shutdown

`SIGINT` and `SIGTERM` start a graceful shutdown: the listener closes, and
in-flight requests have **15 seconds** to finish. Give the orchestrator a
`terminationGracePeriodSeconds` above that, or it will `SIGKILL` a process
that was going to exit cleanly.

## Running more than one instance

Two of the three background jobs were built for it. One thing was not, and
one thing needs care at exactly one moment.

### What is safe

**Webhook delivery** and **directory synchronization** both claim work with
`FOR UPDATE SKIP LOCKED`. Two instances reaching the same due directory in
the same minute is a case the query was written for: the second takes a
different row, or none. Nothing is delivered twice and no schedule is run
twice because a second replica exists.

**Sessions** are JWTs signed with `PORTICO_JWT_SECRET`. Any instance can
verify any token, so no sticky sessions, no shared session store, and
nothing to replicate — *provided every instance has the same secret*, which
is the failure mode named in the table above.

### What is not

**The sign-in rate limiter counts per process.** It is a map in memory, so
three replicas means three separate allowances and an effective limit three
times what the setting says.

This is not a defect to route around; it is what the limiter is for.
[Getting in](access-guide.md#the-sign-in-endpoints-have-a-floor-of-their-own) already puts it plainly: Portico
serves plain HTTP and leaves rate limiting to the reverse proxy. The
built-in one is a **floor** — enough that a single instance is not
defenceless, and not enough to be your answer to credential stuffing. Set
the real limit where requests are already being counted in one place, and
watch `portico_sign_in_attempts_total{outcome="bad_credentials"}` to know
whether it needs tightening.

### Migrations, which is the moment that needs care

Every instance applies pending migrations at startup. There is no lock
around it, so when several start against a database that needs migrating,
they race.

What happens is worth stating exactly, because it is better than it sounds.
One wins and starts. The others exit non-zero with a migration error —
something about `goose_db_version` or a duplicate relation — and the
orchestrator restarts them, by which time the winner has finished and they
start normally. **Nothing is half-applied**: every migration in this
repository runs in a transaction, and none carries `NO TRANSACTION`.

So the cost of a rolling upgrade across replicas is some noise in the logs
and a few restarts, not a broken schema. If you would rather not have the
noise:

1. Scale to one, let it migrate, scale back up. The simplest, and it costs a
   moment of reduced capacity rather than downtime.
2. Run one instance ahead of the rest — an init container, or a job — and
   start the others once it is ready.

Either way, **read the release notes before an upgrade that adds a
migration**, because the window in which old and new instances are both
running is a window where both are talking to the new schema.

### Connection pool arithmetic

Each instance opens **at most 25** connections and keeps 5 idle. PostgreSQL
ships with `max_connections = 100`, and something else is usually already
using some of them.

```
4 instances × 25 = 100    ← at the default ceiling, before anything else connects
```

Past three replicas, raise `max_connections`, or put PgBouncer in front. The
failure is not at startup — it is connection errors under load, which is a
harder thing to recognise for what it is.

## TLS

Portico serves plain HTTP and expects something in front of it. Set
`PORTICO_PUBLIC_URL` to the address people actually use: it is what OpenID
Connect discovery advertises, what SAML metadata carries, and what an
external identity provider's redirect URI is built from — all of which have
to match another system's registration character for character.

[Getting in](access-guide.md) has a worked nginx configuration, including
the rate limiting this page just deferred to it.

## Metrics

Unset by default. `PORTICO_METRICS_ADDR=:9090` opens a second listener that
serves `/metrics` and nothing else — a separate port so that scraping does
not go through whatever authenticates the API, and so that exposing metrics
internally does not mean exposing them publicly.

What is worth alerting on is in [Getting in](access-guide.md#metrics).

## Sizing

The binary is a single Go process with the console and the manual embedded;
it holds no cache and no queue. In practice the memory floor is tens of
megabytes and the thing that moves it is concurrency, not the size of the
tenant.

The one CPU cost worth knowing about is **password hashing**, which is
deliberate: sign-in is the expensive path by design. A burst of sign-ins is
what makes this process work, and it is bounded by the rate limiter and by
your proxy long before it is bounded by the pod.

Start with 256Mi and 500m, watch, and adjust. Numbers more specific than
that from a project this young would be invented rather than measured.

## Kubernetes

`deploy/k8s/` holds a Deployment, a Service, and an example Secret. They are
a starting point rather than a chart: the probes, the grace period and the
environment are the parts that are easy to get wrong, and they are already
right there.

They are **schema-checked but have never been applied to a cluster** — this
project has no cluster to apply them to. Read them before you use them.

## What this page does not cover

- **High availability of PostgreSQL.** A single instance is the assumption;
  see [Database conventions](https://github.com/Paraview-RD/portico/blob/main/docs/database-conventions.md).
- **Anything about scaling PostgreSQL itself** — no partitioning, no
  sharding, no read replicas. Read replicas are a plausible later step and
  are not one today.
