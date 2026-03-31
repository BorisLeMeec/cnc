# CNC Fund Analysis — Claude Project

## Purpose

Graph-based analysis of French CNC (Centre National du Cinéma) commission results,
focused exclusively on the **CNC Talent** fund (Fonds d'aide aux créateurs vidéo sur internet).

The goal is to map relationships between jury members, talents/creators, and beneficiary
companies across all commission sessions, then use AI (Google search, etc.) to discover
who knows who and surface potential conflicts of interest.

**Source:** https://www.cnc.fr/professionnels/aides-et-financements/fonds-daide-aux-createurs-video-sur-internet-cnc-talent/resultats-commissions

---

## Project structure

```
cnc/
├── CLAUDE.md
├── go.mod                          (module: cnc, go 1.25)
├── main.go                         (entrypoint — currently empty)
│
├── cmd/
│   └── scrape/
│       └── main.go                 — CLI to run the scraper
│
├── internal/
│   ├── model/
│   │   └── types.go                — all domain types (nodes + edges of the graph)
│   ├── scraper/
│   │   ├── scraper.go              — fetches HTML, sends to Claude Haiku for parsing
│   │   └── urls.go                 — hardcoded list of 33 known commission page URLs
│   └── store/
│       └── graph.go                — file-backed graph: Load, Save, query helpers
│
└── data/
    ├── raw/
    │   └── commissions/            — one JSON per commission session (scraper output)
    │       └── {commission-id}.json
    ├── persons/                    — one JSON per resolved real person (empty for now)
    │   └── {person-id}.json
    ├── companies/                  — one JSON per resolved legal entity (empty for now)
    │   └── {company-id}.json
    └── relationships/              — one JSON per known person↔person link (empty for now)
        └── {person-a-id}_{person-b-id}.json
```

---

## Current state (last updated: 2026-03-31)

### What's DONE

1. **Domain model** (`internal/model/types.go`) — all Go types defined:
   - `Person`, `Company`, `Project`, `Commission` (nodes)
   - `JuryPresence`, `Grant`, `Relationship` (edges)
   - Enums: `JuryRole`, `AidSection`, `AidType`, `Result`, `RelationshipType`, `Confidence`

2. **Graph store** (`internal/store/graph.go`) — file-backed graph with:
   - `Load(dataDir)` / `Save*()` functions
   - Index builders: `GrantsByTalentPersonID`, `JuryByPersonID`, `RelationshipIndex`
   - Query helpers: `JuryCommissions`, `TalentGrants`, `EvaluatorOf`,
     `PersonsWhoEvaluatedTalent`, `KnownRelationship`, `ConflictsOfInterest`

3. **Scraper** (`internal/scraper/` + `cmd/scrape/`) — fully working:
   - `urls.go`: 33 known CNC Talent commission page URLs (2017–2025)
   - `scraper.go`: fetches HTML → strips to text → sends to Claude Haiku 4.5 → parses JSON response
   - Handles consolidated pages (one URL → multiple `Commission` objects)
   - Skips already-scraped files → safe to re-run
   - Run with: `go run ./cmd/scrape -data data` (needs `ANTHROPIC_API_KEY` env var)
   - Single-URL mode: `go run ./cmd/scrape -data data -url <url>`

4. **Scraped data**: 27 commission sessions in `data/raw/commissions/`:
   - 2017: 1 session (dec)
   - 2018: 4 sessions (mar, apr, jun, oct) — **missing: dec 2018**
   - 2019: 3 sessions (feb, apr, jun) — **missing: oct 2019**
   - 2020: 4 sessions (jan, mar, jun, oct) — **missing: dec 2020**
   - 2021: 4 sessions (feb, apr, jun, oct) — **missing: dec 2021**
   - 2022: 1 session (apr) — **missing: feb, jun, oct 2022**
   - 2023: 2 sessions (apr, sep) — **missing: feb, jun, nov 2023**
   - 2024: 4 sessions (mar, apr, jun, sep) — **missing: nov 2024**
   - 2025: 4 sessions (may, jul, sep, nov) — complete

### What's MISSING from the scrape

~11 sessions are missing. These were on **consolidated year-pages** (one URL
contains multiple commission sessions). The scraper ran but some consolidated
pages may have only returned one session, or errored.

**To fix:** re-run the scraper (it skips existing files), then manually check
consolidated URLs flagged with `Consolidated: true` in `urls.go`. If the page
truly has multiple sessions, the Claude prompt should extract them all —
if not, try re-running just that URL with `go run ./cmd/scrape -data data -url <url>`.

Consolidated URLs to check:
- `_2130283` (2023, should have up to 4 sessions)
- `_1829503` (2022, should have up to 4 sessions)
- `_1628468` (2021, should have up to 5 sessions)
- `_1395148` (2020, should have up to 5 sessions)
- `_1097133` (2019, should have up to 4 sessions)
- `_2322707` (2024, should have up to 5 sessions)

---

## Domain model (internal/model/types.go)

### Nodes

| Type | What it represents |
|---|---|
| `Person` | A real individual — jury member, talent/creator, or both |
| `Company` | A legal entity that receives grant money as beneficiary |
| `Project` | A content project submitted for funding |
| `Commission` | A single jury session (owns `[]JuryPresence` + `[]Grant`) |

### Edges

| Type | Connects | Key fields |
|---|---|---|
| `JuryPresence` | Person ↔ Commission | `role` (président / président suppléant / membre) |
| `Grant` | Project ↔ Commission | `talent_raw/person_id`, `beneficiary_raw/company_id`, `amount`, `aid_section`, `aid_type`, `result` |
| `Relationship` | Person ↔ Person | `type`, `source`, `confidence` — populated via external research |

### Grant taxonomy

**`aid_section`** (top-level section on the CNC page):
- `aide_creation` — individual content projects
- `aide_chaine` — channel/platform development

**`aid_type`** (sub-type within a section):
- `standard` — main creation grant (up to €30,000)
- `bourse_encouragement` — encouragement grant (€2,000)
- `aide_pilote` — pilot episode grant (€5,000)
- `developpement_chaine` — channel development (up to €50,000)

---

## ID conventions

IDs are human-readable kebab-case slugs.

| Entity | Format | Example |
|---|---|---|
| Commission | `cnc-talent-{YYYY-MM-DD}` | `cnc-talent-2025-11-12` |
| Grant | `{commission-id}-{short-title}` | `cnc-talent-2025-11-12-balade-mentale` |
| Person | `{firstname}-{lastname}` | `benjamin-bonnet` |
| Company | slugified company name | `eigengrau-production` |
| Relationship | `{person-a-id}_{person-b-id}` (IDs sorted A→Z) | `benjamin-bonnet_kloe-lang` |

---

## Resolution pattern (raw → resolved)

The scraper stores data exactly as written on the CNC page (zero interpretation).
Resolution is a separate, manual-or-AI-assisted step:

```
talent_raw: "Balade mentale"          → talent_person_id: "thomas-dupont"
beneficiary_raw: "Eigengrau production" → beneficiary_company_id: "eigengrau-production"
jury raw_name: "Kloé Lang"            → person_id: "kloe-lang"
```

`person_id` / `company_id` / `beneficiary_company_id` fields are left `""` until resolved.
Never delete `raw_name` / `talent_raw` / `beneficiary_raw` — they are the source of truth.

---

## Graph query capabilities (internal/store/graph.go)

- `g.JuryCommissions(personID)` — all sessions where a person sat on the jury
- `g.TalentGrants(personID)` — all grants where a person was the talent
- `g.EvaluatorOf(talentPersonID)` — map of commissionID → jury members who evaluated this talent
- `g.PersonsWhoEvaluatedTalent(talentPersonID)` — flat set of jury person IDs
- `g.KnownRelationship(a, b)` — retrieve a relationship between two persons
- `g.ConflictsOfInterest()` — cross-join: jury member with a known relationship to a talent they evaluated

---

## Scraper details (internal/scraper/)

### How it works
1. Fetches raw HTML from CNC page (`net/http`)
2. Strips `<script>`, `<style>`, `<nav>`, `<header>`, `<footer>` to reduce tokens
3. Sends plain text to **Claude Haiku 4.5** (`claude-haiku-4-5`) with a precise system prompt
4. Claude returns a JSON array of `Commission` objects
5. Each commission is written to `data/raw/commissions/{id}.json`

### Running it
```bash
# All known URLs (skips already-scraped):
ANTHROPIC_API_KEY=... go run ./cmd/scrape -data data

# Single URL:
ANTHROPIC_API_KEY=... go run ./cmd/scrape -data data -url "https://..."
```

### Key design choices
- **Claude API for parsing** (not CSS selectors) — the HTML format changed across 8 years of pages
- **Haiku** model — cheap and fast, accurate enough for structured extraction
- **1-second delay** between requests to be polite to CNC servers
- **Idempotent** — skips existing files, safe to re-run

---

## Planned work (next steps, in order)

### Step 1: Complete the scrape (IMMEDIATE NEXT)
Fix the ~11 missing sessions from consolidated pages. Re-run scraper, manually
check consolidated URLs. See "What's MISSING" section above.

### Step 2: Data quality check
After all sessions are scraped, write a quick CLI command to:
- Count total sessions, total grants, total unique talent_raw, total unique jury members
- Flag any anomalies (empty jury, zero grants, duplicate IDs)

### Step 3: Person resolver
Map `talent_raw` values + jury `raw_name` values to `Person` records:
- Many jury members are already real names → auto-slug to person ID
- Many talents are pseudonyms/channel names → need AI research (Google search)
  to find the real person behind the channel
- Create `data/persons/{person-id}.json` files
- Fill in `person_id` / `talent_person_id` fields in commission JSONs

### Step 4: Company resolver
Map `beneficiary_raw` values to `Company` records:
- Create `data/companies/{company-id}.json` files
- Fill in `beneficiary_company_id` fields

### Step 5: Relationship enrichment
AI-assisted discovery of who knows who:
- Google search jury members → find their professional backgrounds, YouTube channels,
  social media, past projects, company affiliations
- Cross-reference with talents: did jury member X produce content with talent Y?
  Did they work at the same company? Appear at the same events?
- Populate `data/relationships/` with confidence-scored links
- Use `claude-sonnet-4-6` for research tasks

### Step 6: Analysis & conflict detection
- Run `g.ConflictsOfInterest()` to find jury↔talent pairs with known relationships
- Recurring beneficiary tracking (same company getting funded repeatedly)
- Funding flow analysis: total amounts per talent, per company, per jury composition
- Jury member influence: who approved what, how many times

### Step 7: Visualisation
- Export graph to Gephi / D3 / Neo4j-compatible format
- Interactive network graph of relationships
- Timeline view of commissions with jury/talent connections

---

## Claude guidance

- **Do not scrape other CNC fund types** — only CNC Talent is in scope.
- **Preserve `raw_*` fields** — never overwrite with resolved values; always fill the `*_id` field instead.
- **Keep commission JSON files faithful to the source page** — no inference or normalisation in the raw layer; do that in code.
- **Use the Claude API for AI-assisted steps** (person resolution, relationship discovery) — prefer `claude-sonnet-4-6` for research tasks, batch requests where possible.
- **Store all amounts as integers in euros** (e.g. `30000`, not `"30 000 €"`).
- **Dates are ISO 8601** (`YYYY-MM-DD`) throughout.
- **Relationship files use sorted IDs** — always sort `person_a_id` < `person_b_id` alphabetically before writing.
- **Scraper uses Claude Haiku 4.5** for HTML→JSON extraction (cheap, fast, good enough).
- **`ANTHROPIC_API_KEY`** must be set as env var or passed with `-key` flag to run the scraper.

---

## Key observations from the data

- **Jury composition changes per session** — same person can have different roles
  (e.g., Benjamin Bonnet: président in Nov 2025, membre in Sep 2025)
- **"Talent" field is often a pseudonym/channel name**, not a real name → needs resolution
- **Beneficiary company ≠ talent person** — money goes to the company
- **Some companies appear across multiple sessions** (e.g., PANDORA Création, URBANIA PRODUCTIONS)
- **~38 total sessions** from Dec 2017 to Nov 2025, 27 already scraped
