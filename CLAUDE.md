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
├── main.go                         (entrypoint — add CLI commands here)
│
├── internal/
│   ├── model/
│   │   └── types.go                — all domain types (nodes + edges of the graph)
│   └── store/
│       └── graph.go                — file-backed graph: Load, Save, query helpers
│
└── data/
    ├── raw/
    │   └── commissions/            — one JSON per commission session (scraper output)
    │       └── {commission-id}.json
    ├── persons/                    — one JSON per resolved real person
    │   └── {person-id}.json
    ├── companies/                  — one JSON per resolved legal entity
    │   └── {company-id}.json
    └── relationships/              — one JSON per known person↔person link
        └── {person-a-id}_{person-b-id}.json
```

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

## Planned work (not yet built)

1. **Scraper** — fetch and parse all CNC Talent commission pages into `data/raw/commissions/`
2. **Person resolver** — map `talent_raw` values to `Person` records (many are obvious real names)
3. **Relationship enrichment** — AI-assisted Google search on jury members to find professional backgrounds and connections to creators
4. **Analysis CLI** — conflict-of-interest reports, funding flow charts, recurring beneficiary tracking
5. **Network graph export** — export to Gephi / D3 / Neo4j-compatible format for visualisation

---

## Claude guidance

- **Do not scrape other CNC fund types** — only CNC Talent is in scope.
- **Preserve `raw_*` fields** — never overwrite with resolved values; always fill the `*_id` field instead.
- **Keep commission JSON files faithful to the source page** — no inference or normalisation in the raw layer; do that in code.
- **Use the Claude API for AI-assisted steps** (person resolution, relationship discovery) — prefer `claude-sonnet-4-6` for research tasks, batch requests where possible.
- **Store all amounts as integers in euros** (e.g. `30000`, not `"30 000 €"`).
- **Dates are ISO 8601** (`YYYY-MM-DD`) throughout.
- **Relationship files use sorted IDs** — always sort `person_a_id` < `person_b_id` alphabetically before writing.
