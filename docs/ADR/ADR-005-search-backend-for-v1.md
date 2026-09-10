# ADR-005: Search Backend for V1: PostgreSQL Full-Text vs. Dedicated Engine

**Status:** Accepted
**Date:** 2026-09-05
**Resolves:** O-004 / G-04
**Constrained by:** D-004, D-008, D-011, D-015, D-018, D-019

## Context

Search/Discovery must support Slice A (keyword + map discovery of listings and related marketplace entities) without becoming a second source of truth. O-004 asked whether V1 should use PostgreSQL full-text search or a dedicated lightweight engine (Typesense, Meilisearch, or similar).

Frozen constraints already bound the choice:

- PostgreSQL + PostGIS is the canonical store for all domain state (D-004). Canonical listing, business, category, and geography data must not live in a search engine.
- OpenSearch, and any dedicated full-text search engine, is deferred until PostgreSQL full-text search has been measured and found insufficient (D-018). Adoption requires a later measurement-backed ADR. This prohibition covers V1 and V1.5.
- H3 geospatial indexing is deferred until PostGIS has been measured and found insufficient (D-019).
- Search must not query or write another domain’s tables (D-015). Listings must not own a search index.
- Architecture allows a derived search index populated asynchronously (outbox preferred). Search index lag is acceptable for discovery. Trust and verification status used for authorization or regulated checks must be read from PostgreSQL via owning-domain contracts, never from a stale search document.
- AI may assist search ranking later (D-011) but must not be the product’s first search path.

Need/Matching provider ranking (Slice B) is a different problem, owned by Needs/Matching. This ADR covers marketplace discovery search, not match ranking.

## Decision

**V1 Search/Discovery uses PostgreSQL full-text search plus PostGIS structured and geo queries. No dedicated search engine (OpenSearch, Typesense, Meilisearch, or equivalent) is introduced in V1 or V1.5.**

Structured search (keywords, filters, geo/viewport) is the V1 path. Natural-language / conversational AI search is out of V1 scope.

### Search domain ownership

Search/Discovery (MARKETPLACE) owns:

- The public discovery query API: `Search`, `SearchNearby`, `GetMapPins` (and future equivalent contracts).
- Query parsing into structured clauses: text, filters, geo, sort, pagination cursor.
- The **derived** search read model in PostgreSQL (projection tables / materialized search documents owned by Search). This is not canonical data.
- Discovery ranking for search result order (relevance + allowed structured signals).
- Rebuild and incremental refresh of its own projection.

Search/Discovery does **not** own:

- Canonical listing, business, service, need, category, or geography records.
- Publish / expire / close lifecycle (Listings, Business Profiles, Services).
- Trust verdicts, EİDS verification, moderation decisions.
- Need/Matching eligibility and provider ranking (`FindEligibleProviders`, `RankProviders`).

Owning domains expose searchable snapshots through contracts (and outbox events for changes). Search never `SELECT`s from another domain’s schema.

### Postgres full-text and structured filter responsibilities

**Full-text (PostgreSQL):** tokenized text over fields the projection is allowed to hold (title, description excerpt, business name, category labels as denormalized text). Implementation uses PostgreSQL full-text facilities (`tsvector` / `tsquery` or equivalent). Exact operator and dictionary choices are implementation items (see Open items).

**Structured filters (PostgreSQL):** equality and range predicates applied in the same query as text and geo — at minimum:

- Entity type (listing, business profile; additional types only when a source contract exists)
- Category (via Master Data identifiers already resolved by the source domain)
- Lifecycle/visibility status as supplied by the source domain (Search indexes only what the owner marks searchable)
- Optional numeric/attribute filters present on the projection (e.g. price band) when the owning domain publishes them

Filters are first-class. Text query without filters is allowed; NL interpretation of a free-form sentence into filters is not V1.

**Visibility:** Search returns only documents the source domain has published as searchable. Unpublished, closed, expired, or moderation-hidden entities are removed from the projection when the owner emits that fact. Search does not invent visibility rules that contradict the owner.

### Geo / viewport search (PostGIS)

All geo search uses PostGIS on geometry stored **in the Search projection** (copied from Location-resolved coordinates / areas supplied by the owner). Search does not become the authority for boundaries; Location remains the geography owner.

V1 geo modes:

- **Nearby:** point + radius (`ST_DWithin` or equivalent) with the same structured filters.
- **Viewport / map:** bounding envelope (`ST_MakeEnvelope` / `ST_Intersects`) for `GetMapPins` and map-constrained `Search`.

H3 (and any hex-index substitute) is not used. If PostGIS plans become insufficient at scale, that is a measured trigger for a future ADR (D-019), not a V1 design.

Distance may be a sort key or a filter. Distance is not a substitute for category or visibility filters.

### Ranking boundary

**Discovery ranking (this domain):** order of Search/Discovery results. V1 ranking is **deterministic and structured**: full-text rank, then documented tie-breakers (e.g. recency, distance when geo-constrained). Exact weight formula is an implementation item (see Open items). AI-assisted re-ranking may be added later as an optional signal (D-011); it must not be the only ranking path and must not run before a structured candidate set exists.

**Match ranking (not this domain):** Needs/Matching owns provider eligibility and `RankProviders`. Search must not rank providers for a Need, and Needs/Matching must not own public listing map search.

**Trust in ranking/filters:** a denormalized trust/verification **badge** on the projection may be used as a discovery filter or weak rank signal, with lag allowed. Any operation that depends on current trust or EİDS status (publish eligibility, regulated actions, authorization) must call Trust / Compliance contracts against PostgreSQL. The search document is never authoritative for those reads.

### Pagination strategy direction

V1 pagination is **keyset / cursor** over a stable sort (rank, then unique id; or distance, then unique id). Offset/limit paging is not the architectural direction for geo + ranked discovery (unstable windows under concurrent writes).

Page size, cursor encoding, and deep-page cutoffs are implementation items — not specified here.

Map pin responses may cap density by viewport; clustering algorithm and pin limits are implementation items.

### Autocomplete / synonym boundary

**V1 in scope:** prefix / simple-term suggestion against the Search projection (titles and names already indexed). This stays in PostgreSQL (e.g. prefix `tsquery`, trigram, or equivalent). Choice of operator is an implementation item.

**Out of V1 / not implied by this ADR:** dedicated suggester services, synonym graphs, query expansion dictionaries, typo-correction engines, and multi-language analyzer parity beyond what PostgreSQL full-text provides for the frozen language set. Synonym and analyzer policy is an OPEN implementation item (see Open items).

Autocomplete does not bypass visibility or structured filters.

### Search read models

V1 **requires** a Search-owned **derived read model inside PostgreSQL**. It is justified because D-015 forbids querying Listings/Business tables directly, while D-018 forbids a dedicated search cluster.

Properties of the read model:

- Disposable and rebuildable from owning-domain contracts.
- Holds only fields needed to filter, rank, snippet, and pin — not a full entity clone.
- Includes `tsvector` (or equivalent) and PostGIS geometry for indexed documents.
- May lag canonical state. Lag is acceptable for discovery.
- Must not be written by Listings or Business business logic except through Search’s ingest contract / outbox consumer.
- Must not be treated as source of truth for any field. Detail views load canonical entities via the owning domain after the user selects a result.

Valkey may cache query results as a derived cache (D-005). Cache miss falls back to PostgreSQL. Cache is not a search index of record.

### Future OpenSearch (or dedicated engine) extraction trigger

A dedicated search engine may be introduced **only** after a new ADR that cites **measured** production evidence that PostgreSQL full-text and/or PostGIS discovery queries are insufficient.

**Measurable triggers are OPEN.** This ADR does not invent QPS, document-count, latency, recall, or SLO thresholds. Future work must define what to measure (query latency, relevance/recall on labeled sets, geo query plans, facet cost) and the evidence bar. Until that ADR exists, OpenSearch and equivalents remain prohibited for V1 and V1.5 (D-018).

If extracted later, the engine remains a **derived** replica. PostgreSQL stays canonical. Trust/EİDS reads still do not use the search engine as authority.

### Rebuild / reindex model

- **Incremental:** owning domains emit outbox events (preferred) on searchable create/update/unpublish/delete; Search upserts or deletes projection rows idempotently by `(entity_type, entity_id)`.
- **Full rebuild:** Search requests current searchable snapshots from owning-domain contracts and replaces the projection. Rebuild is the recovery path after ingest bugs or schema changes.
- Rebuild **never** writes back into owning domains. Projection → canonical reverse sync is forbidden.
- In-process events may invalidate Valkey search caches; they are not the reliability path for projection updates (outbox preferred, per architecture).

### No derived search store as source of truth

Forbidden:

- Treating search projection, Valkey search cache, or a future OpenSearch index as canonical listing/business/geo/trust state.
- Writing domain state only into a search store.
- Serving trust/verification/authorization decisions from search documents.
- Introducing H3 or a dedicated FTS engine in V1/V1.5 without a measurement-backed ADR.

## Consequences

### Positive

- Resolves O-004 / G-04: V1 Search/Discovery can be specified and implemented against PostgreSQL + PostGIS only.
- Preserves D-004, D-018, D-019 and local-first development (no hosted search cluster).
- Keeps D-015: Search owns a projection; it does not raid other schemas.
- Separates discovery ranking from Need/Matching ranking.
- Leaves a clean extraction path to OpenSearch later without rewriting canonical data.

### Negative / Trade-offs

- PostgreSQL full-text and facet/geo joins may prove insufficient at unknown future scale; extraction is delayed until measurement exists.
- Projection lag means discovery can show stale titles, pins, or badges until ingest catches up.
- Dual-write complexity (canonical + projection) is real; rebuild must be operationally routine.
- V1 relevance will be weaker than a dedicated search stack (synonyms, typo-tolerance, NL). That is accepted in order to keep structured search first.

### Constraints imposed

- V1 and V1.5 must not add OpenSearch, Typesense, Meilisearch, H3, or equivalent.
- Natural-language AI search is not the V1 discovery interface.
- Search projection is derived only; never source of truth.
- Cross-domain ingest only via contracts + outbox (or equivalent owner-exported snapshot for rebuild).
- Dedicated-engine adoption requires a new ADR with measured evidence; numeric trigger values remain OPEN until then.

## Open items

These are **not** Phase 0A architecture reversals. They do not re-open D-018 or D-019.

1. **Measurable extraction triggers** — what metrics, sample, and evidence bar justify a dedicated search engine or H3; numeric thresholds and SLOs are unspecified.
2. **PostgreSQL FTS configuration** — dictionaries, stemming, language configs per TR/EN/RU/AR, ranking function weights.
3. **Synonym / analyzer policy** — whether V1 ships any synonym lists; how Arabic/RTL text search is configured (locale architecture is elsewhere; search analyzer mapping is still open).
4. **Autocomplete operator** — prefix `tsquery` vs trigram vs other PostgreSQL facility.
5. **Cursor encoding, page size, pin density/clustering limits.**
6. **Exact V1 searchable entity set beyond listings and business profiles** — e.g. whether Services are first-class documents or only reached via business/listing contracts.
7. **Projection schema details** — columns, GIN/GiST index DDL, snippet fields.
8. **Ingest payload contracts** — exact outbox event names and snapshot DTOs from Listings / Business Profiles / Master Data.
9. **Optional AI re-ranking** — if/when introduced, model choice and how it combines with structured rank (must remain assistive).
10. **Management Center search-ops views** — rebuild controls, lag inspection (required as a surface per D-020 when Search is implemented; UI model follows G-09).
