// Package store provides a simple file-backed graph of CNC commission data.
//
// Directory layout:
//
//	data/
//	  persons/
//	    {person-id}.json          — one Person per file
//	  companies/
//	    {company-id}.json         — one Company per file
//	  raw/
//	    commissions/
//	      {commission-id}.json    — one Commission per file (includes jury + grants)
//	  relationships/
//	    {person-a-id}_{person-b-id}.json  — one Relationship per file
//
// The Graph struct is the in-memory representation. Load() reads all files
// into it; Save() writes back any mutations.
//
// All IDs are human-readable slugs so the files are easy to inspect and edit
// manually (useful for correcting person resolution, adding relationships, etc.)
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cnc/internal/model"
)

// Graph is the full in-memory graph.
type Graph struct {
	Persons       map[string]*model.Person       // keyed by Person.ID
	Companies     map[string]*model.Company      // keyed by Company.ID
	Commissions   map[string]*model.Commission   // keyed by Commission.ID
	Relationships []*model.Relationship

	// Derived indexes (populated by BuildIndexes after Load).

	// GrantsByTalentPersonID: person_id → all grants where they are the talent.
	GrantsByTalentPersonID map[string][]*model.Grant
	// JuryByPersonID: person_id → all jury presences.
	JuryByPersonID map[string][]*juryEntry
	// RelationshipIndex: "a_id|b_id" → relationship (IDs sorted).
	RelationshipIndex map[string]*model.Relationship
}

// juryEntry pairs a JuryPresence with its Commission for easy traversal.
type juryEntry struct {
	Commission *model.Commission
	Presence   model.JuryPresence
}

// NewGraph returns an empty graph.
func NewGraph() *Graph {
	return &Graph{
		Persons:       make(map[string]*model.Person),
		Companies:     make(map[string]*model.Company),
		Commissions:   make(map[string]*model.Commission),
		Relationships: nil,
	}
}

// Load reads all data files from dataDir into the graph.
func Load(dataDir string) (*Graph, error) {
	g := NewGraph()

	loaders := []struct {
		glob string
		fn   func([]byte) error
	}{
		{
			filepath.Join(dataDir, "persons", "*.json"),
			func(b []byte) error {
				var p model.Person
				if err := json.Unmarshal(b, &p); err != nil {
					return err
				}
				g.Persons[p.ID] = &p
				return nil
			},
		},
		{
			filepath.Join(dataDir, "companies", "*.json"),
			func(b []byte) error {
				var c model.Company
				if err := json.Unmarshal(b, &c); err != nil {
					return err
				}
				g.Companies[c.ID] = &c
				return nil
			},
		},
		{
			filepath.Join(dataDir, "raw", "commissions", "*.json"),
			func(b []byte) error {
				var c model.Commission
				if err := json.Unmarshal(b, &c); err != nil {
					return err
				}
				g.Commissions[c.ID] = &c
				return nil
			},
		},
		{
			filepath.Join(dataDir, "relationships", "*.json"),
			func(b []byte) error {
				var r model.Relationship
				if err := json.Unmarshal(b, &r); err != nil {
					return err
				}
				g.Relationships = append(g.Relationships, &r)
				return nil
			},
		},
	}

	for _, l := range loaders {
		matches, err := filepath.Glob(l.glob)
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", path, err)
			}
			if err := l.fn(data); err != nil {
				return nil, fmt.Errorf("parsing %s: %w", path, err)
			}
		}
	}

	g.BuildIndexes()
	return g, nil
}

// BuildIndexes populates the derived lookup maps. Call after any mutation.
func (g *Graph) BuildIndexes() {
	g.GrantsByTalentPersonID = make(map[string][]*model.Grant)
	g.JuryByPersonID = make(map[string][]*juryEntry)
	g.RelationshipIndex = make(map[string]*model.Relationship)

	for _, c := range g.Commissions {
		commission := c
		for _, jp := range c.Jury {
			if jp.PersonID != "" {
				g.JuryByPersonID[jp.PersonID] = append(
					g.JuryByPersonID[jp.PersonID],
					&juryEntry{Commission: commission, Presence: jp},
				)
			}
		}
		for i := range c.Grants {
			grant := &c.Grants[i]
			if grant.TalentPersonID != "" {
				g.GrantsByTalentPersonID[grant.TalentPersonID] = append(
					g.GrantsByTalentPersonID[grant.TalentPersonID], grant,
				)
			}
		}
	}

	for _, r := range g.Relationships {
		key := relKey(r.PersonAID, r.PersonBID)
		g.RelationshipIndex[key] = r
	}
}

// SaveCommission writes a single Commission to disk.
func SaveCommission(dataDir string, c *model.Commission) error {
	return writeJSON(filepath.Join(dataDir, "raw", "commissions", c.ID+".json"), c)
}

// SavePerson writes a single Person to disk.
func SavePerson(dataDir string, p *model.Person) error {
	return writeJSON(filepath.Join(dataDir, "persons", p.ID+".json"), p)
}

// SaveCompany writes a single Company to disk.
func SaveCompany(dataDir string, c *model.Company) error {
	return writeJSON(filepath.Join(dataDir, "companies", c.ID+".json"), c)
}

// SaveRelationship writes a Relationship to disk.
func SaveRelationship(dataDir string, r *model.Relationship) error {
	name := relKey(r.PersonAID, r.PersonBID) + ".json"
	return writeJSON(filepath.Join(dataDir, "relationships", name), r)
}

// ---------------------------------------------------------------------------
// Graph query helpers (building blocks for future analysis)
// ---------------------------------------------------------------------------

// JuryCommissions returns all commissions where personID sat on the jury.
func (g *Graph) JuryCommissions(personID string) []*model.Commission {
	entries := g.JuryByPersonID[personID]
	out := make([]*model.Commission, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Commission)
	}
	return out
}

// TalentGrants returns all grants where personID was the talent.
func (g *Graph) TalentGrants(personID string) []*model.Grant {
	return g.GrantsByTalentPersonID[personID]
}

// EvaluatorOf returns all jury members who sat in the commission where
// personID's project was evaluated.
// Returns: map[commissionID] → []JuryPresence
func (g *Graph) EvaluatorOf(talentPersonID string) map[string][]model.JuryPresence {
	result := make(map[string][]model.JuryPresence)
	for _, grant := range g.TalentGrants(talentPersonID) {
		if c, ok := g.Commissions[grant.CommissionID]; ok {
			result[grant.CommissionID] = c.Jury
		}
	}
	return result
}

// PersonsWhoEvaluatedTalent returns the set of person IDs who were on the jury
// when the given talent's projects were evaluated.
func (g *Graph) PersonsWhoEvaluatedTalent(talentPersonID string) map[string]bool {
	seen := make(map[string]bool)
	for _, presences := range g.EvaluatorOf(talentPersonID) {
		for _, jp := range presences {
			if jp.PersonID != "" && jp.PersonID != talentPersonID {
				seen[jp.PersonID] = true
			}
		}
	}
	return seen
}

// KnownRelationship returns the relationship between two persons, if any.
func (g *Graph) KnownRelationship(personAID, personBID string) *model.Relationship {
	return g.RelationshipIndex[relKey(personAID, personBID)]
}

// ConflictsOfInterest returns all (talent, juror) pairs where the juror sat on
// the commission that evaluated the talent's project AND a known relationship
// exists between them.
func (g *Graph) ConflictsOfInterest() []Conflict {
	var conflicts []Conflict
	for _, c := range g.Commissions {
		for _, grant := range c.Grants {
			if grant.TalentPersonID == "" {
				continue
			}
			for _, jp := range c.Jury {
				if jp.PersonID == "" || jp.PersonID == grant.TalentPersonID {
					continue
				}
				rel := g.KnownRelationship(grant.TalentPersonID, jp.PersonID)
				if rel != nil {
					conflicts = append(conflicts, Conflict{
						Commission:   c,
						Grant:        &grant,
						JuryMember:   jp,
						Relationship: rel,
					})
				}
			}
		}
	}
	return conflicts
}

// Conflict groups a potential conflict-of-interest finding.
type Conflict struct {
	Commission   *model.Commission
	Grant        *model.Grant
	JuryMember   model.JuryPresence
	Relationship *model.Relationship
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func relKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return strings.Join([]string{a, b}, "_")
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
