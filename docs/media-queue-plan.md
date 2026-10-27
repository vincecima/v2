# Miniflux media queue — implementation plan

Prepared October 26, 2026, from the requirements interview.

## Objective

Fork upstream Miniflux to create a personal RSS and media inbox. Reuse its Go application, PostgreSQL storage, feed polling, authentication, API, and existing web interface. Add an exclusive bucket workflow and manual URL capture. Deploy the application and PostgreSQL with Docker Compose on a home server, behind an existing HTTPS reverse proxy.

This document records the agreed implementation scope. See [development status](media-queue.md) for completed work and remaining milestones.

## Confirmed requirements

| Area | Decision |
| --- | --- |
| Base | Upstream Miniflux with PostgreSQL; no SQLite or DuckDB port |
| Users | One user initially; preserve upstream ownership and authentication boundaries |
| Hosting | Home server, Docker, public access through an existing HTTPS reverse proxy |
| Login | Miniflux login |
| Sources | YouTube channel feeds initially; retain ordinary RSS/Atom support |
| Initial history | All existing entries returned by a newly subscribed feed enter Inbox |
| Video types | Accept everything the feed supplies, including Shorts and live-related entries |
| Manual URLs | Dedicated internal Manual source; no feed selection required |
| Duplicate policy | Manual and feed copies of the same URL remain separate |
| Metadata | Title, thumbnail, description, and video duration where available |
| Buckets | Inbox, Queue, Later, Archive, Skipped; exactly one per entry |
| Ordering | Queue and Later sort by date, newest or oldest; no manual ordering |
| Archive | Watched or done; retain entries |
| Skipped | Decided not to watch; retain entries |
| Opening links | Open the original site; do not change bucket |
| Unsubscribe | Stop monitoring while preserving entries and buckets |
| Interface | Keep Miniflux styling and layout; add clickable navigation and actions |
| API | Include workflow operations in the first release |
| Deferred | CLI, new workflow shortcuts, and bulk moves |

## Proposed defaults

These are implementation defaults, not additional interview answers. They can be changed without revisiting the architecture.

- Start with a fresh database. If testing an existing database, backfill existing entries into Inbox without changing their upstream read/unread status.
- Inbox is the landing page. Default to newest-first; allow oldest-first in every bucket and remember the selection per bucket.
- Sort by entry publication date, falling back to capture time for manual links without a known publication date. Use entry ID as a stable tie-breaker. Metadata completion must not unexpectedly change the chosen sorting date.
- Retain all five buckets indefinitely. Permanent deletion is not part of the primary workflow.
- Permit movement from any bucket to any other, including returning archived or skipped items to Inbox.
- Repeated submission of the same normalized URL to the Manual source returns the existing manual entry and preserves its bucket. Do not merge it with entries in other sources.
- Reuse upstream feed discovery and refresh settings initially. An actual channel RSS URL remains a supported fallback if a channel handle cannot be resolved. Do not promise full channel backfill beyond what the feed supplies.
- Missing metadata never prevents saving or opening an item. No YouTube API key, account cookies, video downloading, or mandatory external metadata service is required for initial use.

## Product behavior

New feed entries and newly captured manual URLs enter Inbox. Refreshing a feed may update content but must not change an existing entry's bucket. The first subscription imports the available feed history into Inbox.

Each item exposes Open, Move to Queue, Move to Later, Mark done, and Skip as appropriate. Mark done sets Archive; Skip sets Skipped. A Move to control also permits restoration to any bucket. Opening either an external link or an internal detail view must not change its bucket.

The five bucket links show counts derived from bucket state. Source/category filters remain available. Source and category organize origin; they do not determine workflow status. Source labels remain visible so a manual copy and feed copy are distinguishable.

The interface keeps Miniflux's existing appearance. Show a modest thumbnail, title, source, description excerpt, and duration when known. Do not require a card-grid redesign or a separate frontend. Preserve the existing responsive layout, with usable clickable controls on narrow screens.

Unsubscribe disables future scheduled and manual refreshes through the ordinary UI while retaining the source row and all its entries. Resubscribe reactivates the same source, preserving entry identity and buckets. Source/category management must not accidentally expose destructive cascading deletion as the normal unsubscribe action.

## Implementation design

### Bucket state

Add `entries.bucket`, required with default `inbox` and a database check restricting it to `inbox`, `queue`, `later`, `archive`, or `skipped`. Add an index covering user, bucket, and the normal sort fields after checking query plans.

Keep upstream read/unread and starred state separate. A read-status change must never imply a bucket transition, and a bucket move must not depend on read status. The primary interface uses bucket counts and actions rather than presenting read/unread as a competing workflow.

Implement moves in one shared service/storage path used by web handlers and the API. Scope updates by authenticated user and entry ID. A repeated move to the current bucket succeeds without duplicating anything. Successful writes are atomic; the UI must not report success before persistence succeeds. For simultaneous moves, use last-committed-write behavior and return the persisted state. Serialize moves for an item within the browser so a slower request cannot unintentionally reverse a later click; reloading reads the authoritative server state.

### Manual source

Introduce a source-kind field on feeds, with ordinary feeds as the default and a manual kind for the protected per-user Manual source. Create that source lazily and transactionally on first capture; ensure concurrent captures cannot create two Manual sources. Reuse Miniflux's insertion path without attempting network feed discovery for this source.

Exclude manual sources from scheduling, direct refresh, OPML export, and ordinary subscription deletion. Provide sensible source metadata defaults rather than requiring nullable feeds throughout the application. Protect its containing category from cascading deletion or move the source safely before category removal.

Manual capture validates an HTTP(S) URL, creates an Inbox entry immediately with a usable fallback title, and schedules metadata enrichment. Deduplication remains source-scoped. Apply conservative normalization to avoid collapsing distinct arbitrary URLs; handle common YouTube URL forms by video ID within the Manual source where unambiguous.

### Metadata enrichment

Store nullable thumbnail URL, description, and video duration in seconds as needed after inspecting which upstream fields can be reused. Do not equate estimated reading time with verified video duration. Keep enrichment state, attempt count, and retry timing separately from bucket state.

Use feed metadata when present. For manual links, reuse appropriate upstream fetching/extraction code and add bounded parsing for standard page metadata, structured video data, and provider metadata where supported. Evaluate YouTube title/thumbnail/description/duration extraction against real public examples; duration may remain unknown if it cannot be obtained reliably without credentials.

Run enrichment asynchronously with bounded concurrency and durable pending work in PostgreSQL; avoid introducing Redis or another queue service. Save failures as a retryable metadata condition, with bounded retries and a user-triggered retry. A restart resumes pending work. Updating metadata must never overwrite a later bucket change.

Because the application fetches user-supplied URLs on a home server, apply URL and redirect validation, block unintended private-network metadata requests, bound response sizes and timeouts, and sanitize rendered text/content. Apply equivalent safeguards to server-side thumbnail fetching if introduced. Reuse upstream protections where adequate.

### Retention and subscription lifecycle

Disable age-based entry removal in the deployment configuration and ensure the fork's cleanup paths preserve workflow entries. Keep session and unrelated housekeeping enabled. Explicitly distinguish the product's Archive bucket from upstream functions that archive/remove old rows or create tombstones.

Implement unsubscribe as a retained inactive source, reusing upstream disabled-feed storage where its semantics fit. Check all refresh entry points and source/category deletion paths. Preserve user ownership checks and avoid changing global deletion semantics silently for existing APIs; document any intentionally destructive legacy endpoint and keep it out of the primary UI.

### API contract

Extend the existing authenticated API rather than creating another backend. Proposed contract, to be reconciled with the pinned upstream route definitions:

- Entry responses include bucket and available media metadata.
- Listing supports bucket filtering, ascending/descending date sorting, and pagination.
- A single-entry bucket update validates the bucket and ownership and returns the persisted result.
- Manual capture accepts a URL and returns the new/existing manual entry plus metadata status.
- Metadata retry is available for an owned entry.
- Feed management exposes non-destructive unsubscribe/resubscribe.

Use Miniflux API keys and session authentication as appropriate. Add API documentation and request examples. Preserve existing clients' read/unread behavior where feasible; third-party clients will not gain bucket controls automatically. A custom CLI will use these API operations later.

## Delivery sequence

| Milestone | Work | Completion evidence |
| --- | --- | --- |
| 0. Establish baseline | Select a stable upstream release/commit; create fork and upstream remote; read repository guidance; run baseline checks and local Compose; map relevant storage, UI, API, ingestion, cleanup, and deletion paths | Recorded base commit, reproducible local startup, baseline test results, confirmed migration strategy |
| 1. Buckets and retention | Schema migration, shared move operation, filtered listing/counts, bucket API, cleanup protection, retained unsubscribe/resubscribe | Tests demonstrate exclusivity, ownership, persistence, refresh preservation, and no accidental cleanup/deletion |
| 2. Manual capture and metadata | Protected Manual source, Add URL API, source-scoped duplicates, durable enrichment and fallbacks | URLs save immediately; repeated manual saves preserve bucket; manual/feed copies remain separate; metadata failures do not block use |
| 3. Web workflow | Bucket navigation, counts, per-item actions, date sorting, Add URL form, metadata display, external links, unsubscribe UI | Complete clickable subscribe → triage → open → mark done flow on desktop and a narrow viewport |
| 4. Deployable release | Versioned image, Compose, proxy/login settings, migrations, backup/restore instructions, smoke checks | Fresh install and restart work; authenticated access through the existing proxy; restore rehearsal preserves buckets and sources |

Deliver each milestone in focused changes. Keep database, domain, API, and UI changes aligned without combining the entire fork into one unreviewable patch. Evaluate upstream updates periodically and carry security fixes forward; the chosen version and divergence matter more than keeping every upstream feature in lockstep.

## Acceptance criteria

1. A newly subscribed YouTube feed imports all available entries into Inbox; a later refresh adds new entries without resetting existing buckets.
2. Shorts and livestream-related feed entries are accepted without special filtering.
3. Every entry has exactly one valid bucket, including after simultaneous requests and failed operations.
4. Opening a link or viewing details does not move the item. Mark done moves it to Archive; Skip moves it to Skipped; both can be restored.
5. Queue and Later support newest/oldest sorting with stable pagination and no drag ordering.
6. Manual capture requires only a URL and creates an Inbox item even if metadata extraction fails.
7. Available title, thumbnail, description, and true video duration are displayed; unknown duration is shown as unknown or omitted.
8. Repeating a manual capture does not reset its state. The same video from a feed remains a distinct entry.
9. Metadata retries and worker restarts preserve entries and their bucket changes.
10. Unsubscribing stops refreshes while preserving all source entries, metadata, and buckets. Resubscribing does not duplicate already-known feed items.
11. Cleanup does not remove retained workflow entries. Normal category management cannot silently erase Manual or unsubscribed-source entries.
12. API operations enforce authentication and ownership and match the web workflow.
13. Docker restarts and database backup/restore preserve the queue state. No database port needs public exposure.

Use unit tests for URL normalization, metadata parsing/fallbacks, and state validation; PostgreSQL integration tests for migration, ownership, source creation races, retention, and ingestion; and a small end-to-end workflow check. Use fixtures for automated metadata tests so third-party network behavior does not make CI unreliable. Verify live YouTube examples separately during development.

## Deployment and maintenance

Compose runs the fork and PostgreSQL with persistent database storage. Set the canonical HTTPS base URL for the existing reverse proxy, keep Miniflux login, and use explicit trusted-proxy configuration only where needed. Use environment/secrets configuration for credentials; do not bake them into the image. Publish versioned builds after the repository destination is known.

Back up PostgreSQL before migrations and document restoring a backup with the matching application image. Do not assume an older binary is compatible with a newer schema. Record a tested fresh-install and upgrade procedure for the selected upstream version.

Repository owner/name, hostname, proxy details, and server CPU architecture can be supplied when implementation/deployment begins; they do not block the plan.

## Deferred scope

SQLite/DuckDB, Karakeep integration, embedded playback, playback progress, autoplay, video downloading, manual queue ordering, cross-source deduplication, custom video-type rules, a separate frontend, new workflow keyboard shortcuts, bulk moves, and a CLI are outside the first release. Existing upstream capabilities need not be removed merely because new work on them is deferred.

## Technical references

- [Miniflux upstream](https://github.com/miniflux/v2): starting repository; pin a specific version before implementation.
- [Entry model](https://github.com/miniflux/v2/blob/main/internal/model/entry.go): feed relationship and existing read/starred state.
- [Entry storage](https://github.com/miniflux/v2/blob/main/internal/storage/entry.go): creation/update behavior and feed-scoped identity.
- [Entry queries](https://github.com/miniflux/v2/blob/main/internal/storage/entry_query_builder.go): filtering, feed joins, and ordering.
- [Database migrations](https://github.com/miniflux/v2/blob/main/internal/database/migrations.go): schema and cascading relationships.
- [API reference](https://miniflux.app/docs/api.html): existing entry import, feed management, authentication, and entry operations.
- [Configuration](https://miniflux.app/docs/configuration.html): base URL, API access, database settings, and entry cleanup controls.

The implementation design above is proposed work. Upstream main can change; recheck its assumptions against the selected base commit before writing migrations or relying on existing behavior.
