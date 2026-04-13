# CNC Fund Analysis — Claude Project

## Purpose

Graph-based analysis of French CNC (Centre National du Cinéma) commission results,
focused exclusively on the **CNC Talent** fund (Fonds d'aide aux créateurs vidéo sur internet).

The goal is to map relationships between jury members, talents/creators, and beneficiary
companies across all commission sessions, then surface potential conflicts of interest.

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
│   ├── scrape/
│   │   └── main.go                 — CLI to scrape CNC pages
│   ├── quality/
│   │   └── main.go                 — data quality check report
│   ├── resolve/
│   │   └── main.go                 — AI-based person/talent resolution (Claude Sonnet)
│   ├── link/
│   │   └── main.go                 — fill person_id/talent_person_id in commissions
│   ├── enrich/
│   │   └── main.go                 — Brave Search YouTube co-occurrence for person↔person
│   └── company-enrich/
│       └── main.go                 — Brave Search + Haiku for person↔company connections
│
├── internal/
│   ├── model/
│   │   └── types.go                — all domain types (nodes + edges of the graph)
│   ├── scraper/
│   │   ├── scraper.go              — fetches HTML, sends to Claude Haiku for parsing (streaming)
│   │   └── urls.go                 — hardcoded list of 34 known commission page URLs
│   └── store/
│       └── graph.go                — file-backed graph: Load, Save, query helpers
│
└── data/
    ├── raw/
    │   └── commissions/            — 38 JSON files, one per commission session
    │       └── {commission-id}.json
    ├── persons/                    — 545 JSON files, one per resolved person
    │   └── {person-id}.json
    ├── companies/                  — (empty for now)
    │   └── {company-id}.json
    ├── relationships/              — 24 person↔person links (YouTube co-occurrence + company)
    │   └── {person-a-id}_{person-b-id}.json
    ├── talent_resolution.json      — maps each talent_raw → person (547) or company (55)
    ├── jury_resolution.json        — maps jury raw_name variants → canonical persons (76)
    └── enrichment/
        ├── screen/                 — Haiku screening results (phase 1, low yield)
        ├── youtube/                — Brave Search YouTube co-occurrence results
        │   ├── connections.json    — all matched pairs
        │   ├── progress.json       — resumability tracking
        │   └── matched.json        — pairs already confirmed
        └── companies/
            ├── hits.json           — Brave search hits for jury×company
            └── classifications.json — Haiku classification of hits
```

---

## Current state (last updated: 2026-04-01)

### What's DONE

1. **Scraping** — COMPLETE (38/38 sessions, Dec 2017 → Nov 2025)
   - Scraper uses streaming Claude Haiku API, MaxTokens 32768
   - All consolidated pages handled
   - 722 grants total, ~16M € awarded

2. **Data quality check** (`cmd/quality/`)
   - 38 sessions, 722 grants (all accepted — CNC doesn't publish rejected)
   - 570 unique talent names, 86 unique jury names, 465 unique beneficiaries
   - 7 sessions without a président (labeling variance, not a real issue)

3. **Person resolution** (`cmd/resolve/`)
   - Jury: 103 raw names → 76 unique persons (Claude Sonnet deduplication)
   - Talents: 602 raw names → 547 persons + 55 companies (Claude Sonnet)
   - 545 Person records saved to `data/persons/`
   - Resolution mappings in `talent_resolution.json` and `jury_resolution.json`

4. **Linking** (`cmd/link/`)
   - `person_id` filled on all 316 jury entries
   - `talent_person_id` filled on 652 talent entries (55 companies left empty — expected)
   - Raw fields preserved as source of truth

5. **Relationship enrichment — YouTube co-occurrence** (`cmd/enrich/`)
   - Method: Brave Search API, query `"Person A" "Person B" site:youtube.com`
   - Scope: 24 creator-jury members × all talents they evaluated (same commission)
   - Two passes: primary names (1,484 searches) + aliases (1,372 searches)
   - **24 YouTube collaborations found** → saved to `data/relationships/`
   - Key finding: **Cyprien Iov** had 8 collab matches with talents he evaluated

6. **Relationship enrichment — Company connections** (`cmd/company-enrich/`)
   - Method: Brave Search (general web) + Haiku classification of results
   - Scope: all 76 jury members × 55 companies (same commission only)
   - 676 searches → 33 hits → Haiku classified → **5 real connections**
   - Key finding: **4 jury members connected to Golden Moustache** (Florent Bernard was employee)

### Conflicts of interest found (same-commission, 24 total)

Major ones:
- **Cyprien Iov** (jury 2018): gave grants to 6 collaborators (PV Nova 14K€, Jhon Rachid 25K€,
  Les Parasites 30K€, Solange te parle 50K€, Florence Porcel 25K€, Éléonore Costes 2K€)
- **Florent Bernard** (jury 2018): gave 50K€ to Cyprien, 30K€ to Nicolas Berno — was also
  employee of Golden Moustache which received grants
- **Aude Gogny-Goubert** (jury 2020-2021): gave grants to 4 collab partners — also connected
  to Golden Moustache (not caught because different sessions)
- **Manon Champier** (jury 2022): gave 35K€ to Mamytwink (appeared together on Canal+)
- **Marion Séclin** (jury 2023): gave 50K€ to Lucien Maine
- **Charlie Danger** (jury 2024): gave grants to 3 collab partners

### What's NOT YET DONE

#### Immediate next: Non-same-commission enrichment
Current enrichment only checks jury↔talent pairs within the same commission session.
But conflicts exist across sessions too (e.g., Aude GG ↔ Golden Moustache). Need to:

- **Person↔person**: expand from 1,484 pairs (same commission) to all 11,736 pairs
  (24 creators × 489 talents). ~20,800 Brave searches with aliases. ~8 min, ~$10-15.
- **Person↔company**: expand from 531 pairs (same commission) to all 76 × 55 = 4,180 pairs.
  ~$3-5 for Brave + Haiku classification.

#### Company resolver (beneficiary_raw → Company records)
Map 465 unique `beneficiary_raw` values to `Company` records. Not yet started.
Would enable tracking: same company receiving funds across multiple sessions.

#### Visualization (NEXT STEP)
Build an interactive network graph showing:
- **Nodes**: persons (jury/talent/both), companies
- **Edges**: evaluated (jury→talent), worked_together (YouTube collab),
  employee/business (person→company), funded (company received grant)
- Export as flat JSON for D3.js / Sigma.js consumption

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
| `Relationship` | Person ↔ Person | `type`, `source`, `confidence` — populated via Brave Search + AI |

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

## CLI commands

```bash
# Scrape all CNC pages (skips existing):
export $(cat .env | xargs) && go run ./cmd/scrape -data data

# Scrape single URL:
export $(cat .env | xargs) && go run ./cmd/scrape -data data -url "https://..."

# Data quality report:
go run ./cmd/quality -data data

# Resolve persons (AI — uses Claude Sonnet):
export $(cat .env | xargs) && go run ./cmd/resolve -data data

# Link person_id/talent_person_id in commissions:
go run ./cmd/link -data data

# YouTube co-occurrence enrichment (uses Brave Search):
export $(cat .env | xargs) && go run ./cmd/enrich -data data

# Company enrichment (uses Brave Search + Claude Haiku):
export $(cat .env | xargs) && go run ./cmd/company-enrich -data data
```

---

## Enrichment methodology

### YouTube co-occurrence (person↔person)
1. For each (jury member, talent) pair, search Brave for `"Name A" "Name B" site:youtube.com`
2. Check if top 10 results mention both names in title + description
3. If yes → flag as `worked_together` relationship with YouTube URL as evidence
4. Two passes: primary legal names, then aliases/channel names
5. Only 24 creator-jury members searched (not journalists/executives)

### Company connections (person↔company)
1. Brave Search for `"Person" "Company"` (general web, no site filter)
2. Filter results that mention both in title + description
3. Send matched snippets to Claude Haiku for classification:
   `owns` / `employee` / `business` / `mentioned` (discarded)

### Key design choices
- **Brave Search API** for web search (cheap: $0.005/query, fast: 50 req/s)
- **Co-occurrence** as signal — two names appearing together in search results
- **Alias-aware** — searches both legal names and channel names/pseudonyms
- **Resumable** — progress tracked in JSON files, safe to re-run
- **Two-phase for companies** — cheap search filter, then AI classification
- **API keys** stored in `.env` (ANTHROPIC_API_KEY, BRAVE_API_KEY)

---

## Claude guidance

- **Do not scrape other CNC fund types** — only CNC Talent is in scope.
- **Preserve `raw_*` fields** — never overwrite with resolved values; always fill the `*_id` field instead.
- **Keep commission JSON files faithful to the source page** — no inference or normalisation in the raw layer.
- **Store all amounts as integers in euros** (e.g. `30000`, not `"30 000 €"`).
- **Dates are ISO 8601** (`YYYY-MM-DD`) throughout.
- **Relationship files use sorted IDs** — always sort `person_a_id` < `person_b_id` alphabetically before writing.
- **Scraper uses Claude Haiku 4.5** for HTML→JSON extraction (streaming, 32768 MaxTokens).
- **Enrichment uses Brave Search API** for web/YouTube search, Claude Haiku for classification.
- **`.env`** must contain `ANTHROPIC_API_KEY` and `BRAVE_API_KEY`.

---

## Key observations from the data

- **Jury composition changes per session** — same person can have different roles
  (e.g., Benjamin Bonnet: président in Nov 2025, membre in Sep 2025)
- **20 of 76 jury members were also funded as talents** in other sessions
- **24 creator-jury members** are YouTubers/content creators (not just industry professionals)
- **Golden Moustache** is a hotspot: 4 jury members connected, received grants in 2018-2019
- **Cyprien Iov** had the most conflicts: 8 collaborators received grants while he was jury
- **Same-commission only** catches 24 conflicts, but cross-session analysis would find more
  (e.g., Aude GG ↔ Golden Moustache)
