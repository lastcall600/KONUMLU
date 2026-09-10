# ADR-012: Master Data Architecture (Category Taxonomy, Reference Data, and Pilot Geography Seed)

**Status:** Accepted (architecture)
**Date:** 2026-09-05
**Addresses:** O-007 / G-07 (architecture only — source acquisition and exact taxonomy content remain OPEN)
**Constrained by:** D-004, D-011, D-015, D-017, D-020

## Context

Master Data / Categories is a CORE domain. Architecture already assigns it category taxonomy, reference values (units, condition types, listing types), versioned taxonomies, and pilot geography **seed** coordination. Location / Geo owns PostGIS geometries, boundary polygons, and location resolution.

O-007 still asks two content questions: which dataset supplies Fethiye/Muğla boundaries, and what the initial category tree is. This ADR does **not** invent official or third-party data sources and does **not** freeze a category list. It freezes **how** Master Data is owned, versioned, labelled, published, rolled back, and consumed by Listings, Services, Search, Needs/Matching, and the Management Center.

Without this architecture, V1 would either hardcode forms and filters, collapse into an unbounded EAV store, or duplicate category rules inside Listings (forbidden).

## Decision

**Master Data is the canonical, admin-managed reference domain in PostgreSQL.** It owns versioned taxonomies, attribute definitions, options, synonyms, multilingual labels (TR/EN/RU/AR), location **reference catalog** metadata, and publish lifecycle. **Location owns geometries.** Listings/Services store relational common fields plus **controlled JSONB** instance attributes keyed to a published schema version. **All-EAV is forbidden.**

Exact seed files, boundary vendors, and the concrete category tree are OPEN (see Open items). G-07 remains blocked on those content choices; this ADR unblocks **shape**, not **payload**.

### Domain ownership

**Master Data owns**

- Category / subcategory tree (including applicability: listing, business, service, need)
- Attribute definitions and enumerated options bound to categories (and inherited along the tree unless overridden)
- Shared reference sets (units, condition, listing type, and similar closed lists)
- Synonym sets used for `ResolveCategory` and for search ingest hints
- Location **reference catalog**: stable place codes, labels, provenance, publish state, and **foreign keys to Location region/district IDs** — not PostGIS columns
- Taxonomy schema versions and publish history
- Admin contracts used by the Management Center to mutate the above

**Master Data does not own**

- PostGIS geometry, address normalization, or `ValidateCoordinate` (Location)
- Listing/service/need instance documents or user-generated content
- Search projection tables (Search)
- Trust, EİDS, or moderation verdicts
- The Management Center application itself (MC is the control-plane **surface**; Master Data is the API owner)

**Location owns** polygons and spatial queries. A geography seed job may call Location contracts to upsert geometries and Master Data contracts to upsert catalog rows that **point at** those Location IDs. Master Data must not store canonical geometry.

**Consumers** (Listings, Business Profiles, Services, Needs/Matching, Search) validate `category_id` and attribute payloads **only** through Master Data contracts. They do not copy taxonomy business rules.

AI may **assist** `ResolveCategory` (D-011). The published taxonomy and admin-approved mappings are authoritative. AI must not silently create categories or publish schema versions.

### Categories / subcategories

Relational tree: stable internal ID, immutable **code**, optional parent, sort order, applicability flags, status, current published schema version pointer.

- Depth is bounded by product need; this ADR does not freeze depth.
- A child may add attributes; it must not silently drop a parent’s required attributes without a breaking schema version and migration (below).
- Leaf vs non-leaf posting rules (whether users may post on internal nodes) are an implementation/product item; the model must allow restricting posts to allowed nodes.
- The same tree may classify listings, businesses, and services via applicability flags rather than three disconnected forests, unless a later versioned change splits them. Exact tree content is OPEN.

### Attributes / options

**Definitions** are relational rows: code, value type (text, integer, decimal, boolean, enum, enum-multi, quantity+unit, date — extend only via versioned schema), required/optional, cardinality, validation constraints, filterable/facetable flags, form widget hint, sort order, applicability.

**Enum options** are relational rows under a definition (stable option ID + code + labels + sort). Options are not free strings in JSONB.

**Instance values** (on a listing or service) live in the owning marketplace domain as **controlled JSONB**: keys are attribute **codes** (or IDs) from the referenced schema version; values conform to that version’s types. Unknown keys are rejected on write. Common fields stay **relational** on the owner (at minimum: identity/owner refs, title, description, `category_id`, location ref, status, price/amount when the product uses a first-class price, media refs). Those fields must not be pushed into JSONB as a general pattern.

Forbidden: a generic `attributes(entity_id, key, value)` EAV table as the primary listing model; unconstrained JSON with no schema version; encoding the category tree only inside JSON.

### Location reference data

Master Data holds the **catalog and seed metadata** for pilot places (e.g. which codes are in the Fethiye/Muğla launch set, labels, parents such as il/ilçe/mahalle as **catalog hierarchy**, provenance, publish state).

Location holds **geometry and resolution**. Listing geo queries use Location/PostGIS (and Search’s derived geometry), not Master Data.

Which file, vendor, or official register supplies polygons or gazetteer rows is **OPEN**. This ADR does not name OSM, cadastre, or any other source as selected.

### Business / service taxonomy

Business profiles and services reference Master Data categories through the same versioned contracts (`category_id` / `category_ids` as already implied by architecture). Service catalog forms use the same attribute-definition mechanism as listings, scoped by applicability. Corporate Workspace boundary (O-006) is out of scope for this ADR.

### Stable IDs and codes

- **ID:** surrogate UUID (or equivalent) as the durable foreign key stored on listings/services/needs.
- **Code:** immutable, ASCII, `[a-z0-9][a-z0-9._-]*`, unique within a namespace (category vs attribute vs option vs place). Codes are never recycled to mean something else.
- Labels may change; IDs and codes do not. Rename = label edit. Replace = new code + migration + deprecation of the old node.
- URLs and Search filter params should prefer **codes** for stability across environments; storage FKs prefer **IDs**.

### TR / EN / RU / AR labels

Every user-visible taxonomy node (category, attribute, option, reference value, place catalog name) has labels for **TR, EN, RU, AR** (D-017). Missing translations may fall back per locale policy (implementation; must not ship empty TR for admin-published V1 nodes). Arabic labels are data; RTL layout is a surface concern (ADR-009).

Labels are data in Master Data (not only frontend message catalogs), because they drive forms, filters, and admin.

### Turkish İ / i / ı / I

- **Codes** avoid Turkish casing: ASCII only, stored lowercase, compared as bytes.
- **Labels and synonyms** are UTF-8. Matching, unique-synonym checks, and search hints that involve Turkish **must** use Turkish-aware case mapping (dotted/dotless I). Unicode default casefold is insufficient for TR and is forbidden for TR synonym/category resolve.
- PostgreSQL text search / unique indexes on Turkish labels must not assume English `lower()`.

### Synonyms

Master Data owns synonym lists attached to categories (and optionally attributes/options): language-tagged terms used by `ResolveCategory` and exported to Search as ingest/query hints.

Synonyms do not create categories. They do not bypass publish state. They are versioned with the taxonomy snapshot or as an attached published set referenced by that snapshot.

### Schema / versioning

A **schema version** is an immutable snapshot of: tree slice needed to interpret a category, attribute defs, options, constraints, and synonym set pointer.

- Listings/services store `category_id` + `master_data_schema_version`.
- New posts use the category’s **current PUBLISHED** version.
- Existing posts keep their version until migrated.
- Additive vs breaking changes are distinguished (see migration).

`taxonomy_versions` (already named in architecture) is the system of record for these snapshots.

### Provenance / source metadata

Every seed or imported catalog row records provenance **without assuming a vendor**: source identifier (string), retrieval date, license/notes, importer, checksum if applicable. Geometry provenance lives with Location rows; catalog provenance lives with Master Data rows.

Source acquisition process and allowed sources remain OPEN.

### Lifecycle: DRAFT → REVIEW → APPROVED → PUBLISHED

Schema and material catalog changes follow:

1. **DRAFT** — editable, not used by public forms or new posts.
2. **REVIEW** — locked for edit except by authorized return-to-draft; visible to admins.
3. **APPROVED** — accepted, not yet live.
4. **PUBLISHED** — immutable snapshot; becomes current for its category (or catalog set). Prior published versions remain readable for old listings.

Unpublish / deprecate is a new transition on the **pointer**, not an in-place mutate of a published snapshot. AI cannot advance REVIEW → APPROVED → PUBLISHED.

### Rollback

Rollback **re-points** the category (or catalog set) to a previous PUBLISHED snapshot. It does not delete history. It does not rewrite listing JSONB. Listings on a newer version remain on that version until a forward or reverse migration job runs. Emergency rollback of “current for new posts” is pointer-only and must run impact preview first.

### Impact preview

Before APPROVED → PUBLISHED (and before rollback pointer change), Master Data requests **counts and breakers** through contracts, for example:

- Listings / services / needs on affected `category_id`
- How many sit on schema versions that would become incompatible
- Filterable attributes added/removed (Search rebuild implication)

Preview is advisory and blocking per admin policy (implementation). Master Data must not scan other domains’ tables (D-015).

### Existing-listing (and service) migration when schemas change

| Change class | Rule |
|---|---|
| Additive (new optional attribute, new enum option, label-only) | New PUBLISHED version; existing instances valid; no forced rewrite |
| Breaking (remove/rename code, type change, new required field, option removal still referenced) | New version; existing instances stay on old version; migration job maps JSONB via explicit mapping table; unmapped values fail the job, not silent drop |
| Category move / merge / split | Mapping table; listings keep old `category_id` until migrated; Search/Need eligibility use post-migration IDs only after success |

Migrations are idempotent, auditable, and reversible only via a new mapping (no destructive in-place guess). Needs store `resolved_category_id` and follow the same version/mapping rules when category identity changes.

### Dynamic listing / service forms

Public and MC create/edit forms are **schema-driven**: fetch published attribute definitions for the selected category version; render fields accordingly. Surfaces must not hardcode per-category field lists. Widget hints may guide UI; validation is enforced in the owning domain using Master Data contracts on write.

### Search / filter compatibility

Search/Discovery may facet/filter only on attributes marked filterable in the **published** definition, using **codes** in the Search projection (ADR-005). Schema publish that adds/removes filterable fields implies Search projection rebuild/reindex for affected documents. Search must not invent filters from unconstrained JSONB keys. Category tree filters use published IDs/codes only.

### Admin / Management Center ownership

Master Data is **admin-manageable in V1**. The Management Center is the required operational surface (D-020): taxonomy editor, label/synonym editor, schema version list, lifecycle actions, impact preview, migration job status, provenance view, geography catalog (non-geometry) linked to Location admin for polygons.

Authorization for publish/rollback is a privileged MC role; Master Data enforces the same checks in-domain, not only in the UI. MC frontend app-vs-namespace remains G-09 / O-009 (open). This ADR does not choose the MC front-end topology.

## Consequences

### Positive

- Single owner for taxonomy, forms, filters, and multilingual reference labels.
- PostgreSQL remains source of truth (D-004); no search engine as taxonomy store.
- Avoids all-EAV while allowing category-specific fields via controlled JSONB.
- Publish/rollback/migration are explicit; listings are not silently rewritten.
- Location vs Master Data split keeps PostGIS out of the taxonomy domain.
- Turkish locale correctness is a data constraint, not a UI afterthought.

### Negative / Trade-offs

- Dual version (category + schema_version) on every listing increases consumer complexity.
- Impact preview and migrations require contracts from Listings/Services/Needs/Search before publish is safe.
- O-007 content (sources, tree) still blocks seeding Master Data **payload** even after this architecture ADR.
- Label storage in Master Data overlaps frontend i18n catalogs; consumers must treat Master Data as source for taxonomy strings.

### Constraints imposed

- No all-EAV primary model; no geometry in Master Data; no unofficial “source of truth” search index for categories.
- No in-place mutation of PUBLISHED snapshots; no AI-only publish.
- No invented official geography or taxonomy datasets in this ADR.
- TR case mapping required for TR label/synonym matching; codes remain ASCII.

## Open items

1. **Source acquisition** — who supplies Fethiye/Muğla boundaries and gazetteer rows; licenses; refresh cadence (O-007 remainder). No source is selected here.
2. **Exact taxonomy content** — category tree, attributes, options, units, listing types for V1 (O-007 remainder).
3. **Location catalog vs Location geometry seed procedure** — operational runbook and environment promotion of seed data.
4. **Post-on-non-leaf rules** and maximum tree depth.
5. **Required vs optional translations** at PUBLISHED for EN/RU/AR vs TR.
6. **Synonym matching algorithm** beyond Turkish-aware casefold (edit distance, stopwords).
7. **Impact-preview blocking policy** and migration batching/SLA (no numeric SLOs invented here).
8. **Attribute type catalog extensions** beyond the initial set.
9. **Need-resolution mapping** when AI `ResolveCategory` disagrees with admin synonyms (human override path is required; UX is open).
10. **MC screen inventory and G-09** frontend topology.
11. **Search rebuild triggering** details when filterable attributes change (coordination with ADR-005 ingest).
