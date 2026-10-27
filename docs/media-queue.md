# Media queue development

This is Vince Cima's Miniflux fork for a personal media inbox. The first change establishes backend bucket state and a reproducible local environment. It is not the completed application or the production deployment.

## Baseline

- Upstream: `https://github.com/miniflux/v2`
- Fork: `https://github.com/vincecima/v2`
- Base commit: `d5c529b94ce9a172bd4c22a2b89da068c583783b`
- Latest release when work began: `2.3.4`. The pinned commit also includes subsequent upstream fixes, including authentication/session and forwarded-address handling fixes.
- Go 1.26 and PostgreSQL 17 for local development and queue integration checks.

## Implemented in the foundation

- Every entry has one required bucket: `inbox`, `queue`, `later`, `archive`, or `skipped`.
- New and pre-existing entries default to Inbox. Read/unread and starred state remain independent.
- An atomic, owner-scoped move operation; retries to the same bucket leave the change timestamp alone. Concurrent moves use last-committed-write semantics.
- Bucket-filtered lists and IDs, counts including empty buckets, and stable date pagination using entry ID to break ties.
- Refreshing a feed preserves entry buckets.
- Age-based entry removal is disabled. Legacy history flushing returns HTTP 409, and its buttons are removed. Archive is retained workflow state, not deletion.
- Non-destructive subscription API using the existing disabled flag. Disabled feeds are skipped by scheduled and direct refreshes, including force refresh. A fetch already in progress when unsubscribing can still finish; it saves only refresh metadata and cannot re-enable the feed or overwrite subscription settings.
- Fork migrations have their own `queue_schema_version` ledger. Upstream migration numbers are unchanged.
- PostgreSQL-backed migration/API tests and a ready-to-install GitHub Actions workflow template at `docs/ci/media_queue.yml`. It is not active: the publishing credential lacks GitHub's `workflow` scope. Copy it into `.github/workflows/media_queue.yml` using a credential permitted to edit workflows to enable these CI checks.

The existing UI still shows upstream read/unread navigation. Bucket controls, the Inbox landing page, and the normal non-destructive unsubscribe button are the next UI work. Existing explicit feed/category DELETE operations remain destructive at this stage; do not use them to unsubscribe. Source/category deletion protection is still pending. Manual capture, metadata workers, and public deployment are also pending.

## Local development

From the repository root:

```sh
docker compose -f compose.queue-dev.yml up -d
```

Open `http://localhost:18080`, using `admin` / `queue-dev-only`. This Compose file intentionally binds only to loopback and uses development credentials. It is not the public reverse-proxy configuration. PostgreSQL has no published host port.

After Go edits, restart the app to rebuild:

```sh
docker compose -f compose.queue-dev.yml restart app
```

Run unit tests, race detection, the new PostgreSQL tests, and vet:

```sh
docker compose -f compose.queue-dev.yml run --rm test
```

The queue integration tests use isolated, randomly named schemas and remove them afterward. They require `TEST_DATABASE_URL` to be a PostgreSQL URL with permission to create schemas. Without it, these tests explicitly skip. Existing upstream HTTP integration tests separately require the `TEST_MINIFLUX_*` environment variables described in the Makefile.

Stop services without deleting the database:

```sh
docker compose -f compose.queue-dev.yml down
```

## API additions

All operations use Miniflux's existing authentication. Use `X-Auth-Token` with a key created in Settings; never embed a key in source code.

| Operation | Request | Result |
| --- | --- | --- |
| Bucket counts | `GET /v1/buckets` | Object mapping each bucket to its entry count; all owned entries, including hidden feeds |
| Bucket entries | `GET /v1/entries?bucket=queue&order=published_at&direction=asc&limit=50&offset=0` | Existing entry-list response with `bucket` included on each entry |
| Bucket IDs | `GET /v1/entries/ids?bucket=queue` | Existing IDs response, filtered by bucket |
| Move | `PUT /v1/entries/{id}/bucket`, body `{"bucket":"archive"}` | `{"id":123,"bucket":"archive"}` reflecting the write |
| Unsubscribe | `PUT /v1/feeds/{id}/subscription`, body `{"subscribed":false}` | `{"id":123,"subscribed":false}`; entries retained |
| Resubscribe | Same endpoint, body `{"subscribed":true}` | Reactivates the existing feed |

Bucket filtering also works on feed/category entry lists. Invalid buckets return 400; a missing or unowned entry/feed returns 404; unauthenticated operations return 401. Setting the same bucket again is safe. Existing read-status operations do not move entries between buckets. Entry creation/import remains feed-scoped until the Manual source milestone.

`PUT` and `DELETE /v1/flush-history` now return 409. The old history-flush UI route is also rejected. `CLEANUP_ARCHIVE_READ_DAYS` and `CLEANUP_ARCHIVE_UNREAD_DAYS` cannot override retention in this fork; all five buckets are retained. Explicit account/feed/category deletion is not covered by this guarantee yet.

## Migration and upgrades

Back up PostgreSQL before upgrading. `RUN_MIGRATIONS=1` runs upstream migrations, then the fork migrations. Startup without migrations rejects an absent, older, or newer fork schema. Existing entries are backfilled into Inbox while retaining read/unread state. Re-running migrations is safe.

Do not run an unmodified upstream binary against this fork's data: upstream cleanup does not know our retention rules. Restore a pre-upgrade backup to roll back. Keep the upstream migration list intact when importing upstream updates and put fork changes in `internal/database/queue_migrations.go`.

## Next increments

1. Protect source/category deletion and expose non-destructive unsubscribe in the UI.
2. Add the protected Manual source and durable metadata enrichment, with separate manual/feed copies.
3. Add the five-bucket web workflow, per-item move actions, date sorting, and external open actions.
4. Prepare the home-server image/Compose settings, reverse-proxy configuration, and backup/restore verification.

New shortcuts, bulk moves, manual ordering, playback, and a CLI remain deferred according to the interview.
