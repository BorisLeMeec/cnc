package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cnc/internal/store"
)

// Graph export format for visualization.

type graphExport struct {
	Nodes []node `json:"nodes"`
	Edges []edge `json:"edges"`
}

type node struct {
	ID       string         `json:"id"`
	Label    string         `json:"label"`
	Type     string         `json:"type"` // "jury", "talent", "both", "company"
	Metadata map[string]any `json:"metadata,omitempty"`
}

type edge struct {
	Source   string         `json:"source"`
	Target   string         `json:"target"`
	Type     string         `json:"type"` // "evaluated", "worked_together", "employee", "business", "funded"
	Weight   int            `json:"weight,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func main() {
	dataDir := flag.String("data", "data", "path to data directory")
	outFile := flag.String("out", "data/graph.json", "output file")
	flag.Parse()

	g, err := store.Load(*dataDir)
	if err != nil {
		log.Fatalf("load: %v", err)
	}

	// Track who is jury, who is talent, who is both.
	juryPersons := map[string]bool{}   // person_id → was jury
	talentPersons := map[string]bool{} // person_id → was talent
	juryRoles := map[string]int{}      // person_id → number of sessions as jury
	talentGrants := map[string]int{}   // person_id → number of grants received
	talentAmount := map[string]int{}   // person_id → total € received

	// "evaluated" edges: jury member → talent (aggregated across commissions)
	type evalKey struct{ jury, talent string }
	evalEdges := map[evalKey]*struct {
		count       int
		totalAmount int
		commissions []string
	}{}

	// Company nodes (talent_raw classified as company).
	companyTalents := loadCompanyTalents(*dataDir)

	for _, c := range g.Commissions {
		for _, jp := range c.Jury {
			if jp.PersonID == "" {
				continue
			}
			juryPersons[jp.PersonID] = true
			juryRoles[jp.PersonID]++
		}

		for _, grant := range c.Grants {
			if grant.TalentPersonID != "" {
				talentPersons[grant.TalentPersonID] = true
				talentGrants[grant.TalentPersonID]++
				talentAmount[grant.TalentPersonID] += grant.Amount
			}

			// Build evaluated edges.
			for _, jp := range c.Jury {
				if jp.PersonID == "" || grant.TalentPersonID == "" {
					continue
				}
				key := evalKey{jp.PersonID, grant.TalentPersonID}
				e := evalEdges[key]
				if e == nil {
					e = &struct {
						count       int
						totalAmount int
						commissions []string
					}{}
					evalEdges[key] = e
				}
				e.count++
				e.totalAmount += grant.Amount
				e.commissions = append(e.commissions, c.ID)
			}

			// Track company talents.
			if grant.TalentRaw != "" && companyTalents[strings.ToLower(grant.TalentRaw)] {
				talentPersons[grant.TalentRaw] = true // use raw name as ID for companies
			}
		}
	}

	var export graphExport

	// Build person nodes.
	allPersonIDs := map[string]bool{}
	for id := range juryPersons {
		allPersonIDs[id] = true
	}
	for id := range talentPersons {
		allPersonIDs[id] = true
	}

	for id := range allPersonIDs {
		// Skip company talent_raw entries here — handled separately.
		if companyTalents[strings.ToLower(id)] {
			continue
		}

		person := g.Persons[id]
		label := id
		if person != nil {
			label = person.FullName
		}

		nodeType := "talent"
		if juryPersons[id] && talentPersons[id] {
			nodeType = "both"
		} else if juryPersons[id] {
			nodeType = "jury"
		}

		meta := map[string]any{}
		if juryRoles[id] > 0 {
			meta["jury_sessions"] = juryRoles[id]
		}
		if talentGrants[id] > 0 {
			meta["grants_received"] = talentGrants[id]
			meta["total_amount"] = talentAmount[id]
		}
		if person != nil && len(person.Aliases) > 0 {
			meta["aliases"] = person.Aliases
		}

		export.Nodes = append(export.Nodes, node{
			ID:       id,
			Label:    label,
			Type:     nodeType,
			Metadata: meta,
		})
	}

	// Build company nodes.
	companyGrantCount := map[string]int{}
	companyGrantAmount := map[string]int{}
	for _, c := range g.Commissions {
		for _, grant := range c.Grants {
			lower := strings.ToLower(grant.TalentRaw)
			if companyTalents[lower] {
				companyGrantCount[grant.TalentRaw]++
				companyGrantAmount[grant.TalentRaw] += grant.Amount
			}
		}
	}
	for raw, count := range companyGrantCount {
		export.Nodes = append(export.Nodes, node{
			ID:    "company:" + slugify(raw),
			Label: raw,
			Type:  "company",
			Metadata: map[string]any{
				"grants_received": count,
				"total_amount":    companyGrantAmount[raw],
			},
		})
	}

	// Build "evaluated" edges (jury → talent, aggregated).
	for key, e := range evalEdges {
		// Skip company talents in eval edges for now.
		if companyTalents[strings.ToLower(key.talent)] {
			continue
		}
		export.Edges = append(export.Edges, edge{
			Source: key.jury,
			Target: key.talent,
			Type:   "evaluated",
			Weight: e.count,
			Metadata: map[string]any{
				"total_amount": e.totalAmount,
				"commissions":  e.commissions,
			},
		})
	}

	// Build relationship edges.
	// Company raw names in relationships need to map to "company:slug" node IDs.
	companyNodeID := func(raw string) string {
		if companyTalents[strings.ToLower(raw)] {
			return "company:" + slugify(raw)
		}
		return raw
	}

	for _, r := range g.Relationships {
		edgeType := string(r.Type)
		source := companyNodeID(r.PersonAID)
		target := companyNodeID(r.PersonBID)
		export.Edges = append(export.Edges, edge{
			Source: source,
			Target: target,
			Type:   edgeType,
			Metadata: map[string]any{
				"evidence":   r.Notes,
				"source":     r.Source,
				"confidence": string(r.Confidence),
			},
		})
	}

	// Load cross-links (co-jury-then-evaluated, reciprocal funding).
	crossLinksPath := filepath.Join(*dataDir, "cross_links.json")
	if data, err := os.ReadFile(crossLinksPath); err == nil {
		var crossLinks []struct {
			Type    string `json:"type"`
			PersonA string `json:"person_a"`
			PersonB string `json:"person_b"`
			Details []struct {
				Commission string `json:"commission"`
				Date       string `json:"date"`
				Role       string `json:"role"`
				Amount     int    `json:"amount"`
			} `json:"details"`
		}
		json.Unmarshal(data, &crossLinks)

		for _, cl := range crossLinks {
			// Build evidence summary.
			var parts []string
			totalAmount := 0
			for _, d := range cl.Details {
				totalAmount += d.Amount
				parts = append(parts, fmt.Sprintf("%s: %s", d.Date, d.Role))
			}

			export.Edges = append(export.Edges, edge{
				Source: cl.PersonA,
				Target: cl.PersonB,
				Type:   cl.Type,
				Metadata: map[string]any{
					"total_amount": totalAmount,
					"details":      parts,
				},
			})
		}
	}

	// Load indirect links (same beneficiary conflicts, jury+talent same year).
	indirectPath := filepath.Join(*dataDir, "indirect_links.json")
	if data, err := os.ReadFile(indirectPath); err == nil {
		var indirect struct {
			SameBeneficiary []struct {
				BeneficiaryRaw string `json:"beneficiary_raw"`
				Talents        []struct {
					TalentSlug string `json:"talent_slug"`
					TalentName string `json:"talent_name"`
				} `json:"talents"`
				JuryConflicts []struct {
					JurySlug       string `json:"jury_slug"`
					KnowsTalent    string `json:"knows_talent"`
					EvaluatedOther string `json:"evaluated_other_talent_from_same_company"`
					Commission     string `json:"commission"`
					Amount         int    `json:"amount"`
				} `json:"jury_conflicts"`
			} `json:"same_beneficiary"`
			JuryTalentSameYear []struct {
				PersonSlug   string   `json:"person_slug"`
				PersonName   string   `json:"person_name"`
				JurySessions []string `json:"jury_sessions"`
				Grants       []struct {
					Commission string `json:"commission"`
					Date       string `json:"date"`
					Amount     int    `json:"amount"`
				} `json:"grants_same_year"`
			} `json:"jury_and_talent_same_year"`
		}
		json.Unmarshal(data, &indirect)

		// Same beneficiary jury conflicts only (skip talent↔talent edges — no money involved).
		seen := map[string]bool{}
		for _, sb := range indirect.SameBeneficiary {
			for _, jc := range sb.JuryConflicts {
				key := "sbc:" + jc.JurySlug + "|" + jc.EvaluatedOther + "|" + jc.Commission
				if seen[key] {
					continue
				}
				seen[key] = true
				export.Edges = append(export.Edges, edge{
					Source: jc.JurySlug,
					Target: jc.EvaluatedOther,
					Type:   "same_beneficiary_conflict",
					Metadata: map[string]any{
						"knows":       jc.KnowsTalent,
						"beneficiary": sb.BeneficiaryRaw,
						"commission":  jc.Commission,
						"amount":      jc.Amount,
					},
				})
			}
		}

		// Jury + talent same year: add as edge type.
		for _, jt := range indirect.JuryTalentSameYear {
			totalAmount := 0
			for _, g := range jt.Grants {
				totalAmount += g.Amount
			}
			export.Edges = append(export.Edges, edge{
				Source: jt.PersonSlug,
				Target: jt.PersonSlug, // self-loop — marks dual role
				Type:   "jury_and_talent_same_year",
				Metadata: map[string]any{
					"jury_sessions":  jt.JurySessions,
					"total_received": totalAmount,
				},
			})
		}
	}

	// Sort for deterministic output.
	sort.Slice(export.Nodes, func(i, j int) bool {
		return export.Nodes[i].ID < export.Nodes[j].ID
	})
	sort.Slice(export.Edges, func(i, j int) bool {
		if export.Edges[i].Source != export.Edges[j].Source {
			return export.Edges[i].Source < export.Edges[j].Source
		}
		if export.Edges[i].Target != export.Edges[j].Target {
			return export.Edges[i].Target < export.Edges[j].Target
		}
		return export.Edges[i].Type < export.Edges[j].Type
	})

	data, _ := json.MarshalIndent(export, "", "  ")
	if err := os.WriteFile(*outFile, data, 0o644); err != nil {
		log.Fatalf("write: %v", err)
	}

	// Stats.
	typeCounts := map[string]int{}
	for _, n := range export.Nodes {
		typeCounts["node:"+n.Type]++
	}
	for _, e := range export.Edges {
		typeCounts["edge:"+e.Type]++
	}

	fmt.Printf("Exported graph to %s\n", *outFile)
	fmt.Printf("  Nodes: %d\n", len(export.Nodes))
	for _, t := range []string{"jury", "talent", "both", "company"} {
		if c := typeCounts["node:"+t]; c > 0 {
			fmt.Printf("    %-10s %d\n", t, c)
		}
	}
	fmt.Printf("  Edges: %d\n", len(export.Edges))
	for _, t := range []string{"evaluated", "worked_together", "colleague", "business"} {
		if c := typeCounts["edge:"+t]; c > 0 {
			fmt.Printf("    %-20s %d\n", t, c)
		}
	}
}

func loadCompanyTalents(dataDir string) map[string]bool {
	data, err := os.ReadFile(dataDir + "/talent_resolution.json")
	if err != nil {
		return map[string]bool{}
	}
	var talents []struct {
		TalentRaw string `json:"talent_raw"`
		Type      string `json:"type"`
	}
	json.Unmarshal(data, &talents)

	m := map[string]bool{}
	for _, t := range talents {
		if t.Type == "company" {
			m[strings.ToLower(t.TalentRaw)] = true
		}
	}
	return m
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, "'", "")
	return s
}
