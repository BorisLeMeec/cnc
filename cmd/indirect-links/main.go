package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"cnc/internal/store"
)

type finding struct {
	Type    string `json:"type"`
	Details any    `json:"details"`
}

// --- Type 2: Same beneficiary = hidden connection ---

type sameBeneficiary struct {
	BeneficiaryRaw string         `json:"beneficiary_raw"`
	Talents        []talentGrant  `json:"talents"`
	JuryConflicts  []juryConflict `json:"jury_conflicts,omitempty"`
}

type talentGrant struct {
	TalentSlug string `json:"talent_slug"`
	TalentName string `json:"talent_name"`
	TalentRaw  string `json:"talent_raw"`
	Grants     int    `json:"grants"`
	Total      int    `json:"total_amount"`
}

type juryConflict struct {
	JurySlug       string `json:"jury_slug"`
	JuryName       string `json:"jury_name"`
	KnowsTalent    string `json:"knows_talent"`
	EvaluatedOther string `json:"evaluated_other_talent_from_same_company"`
	Commission     string `json:"commission"`
	Amount         int    `json:"amount"`
}

// --- Type 4: Talent funded same year as jury ---

type juryAndTalentSameYear struct {
	PersonSlug     string      `json:"person_slug"`
	PersonName     string      `json:"person_name"`
	JurySessions   []string    `json:"jury_sessions"`
	GrantsSameYear []grantInfo `json:"grants_same_year"`
}

type grantInfo struct {
	Commission string `json:"commission"`
	Date       string `json:"date"`
	TalentRaw  string `json:"talent_raw"`
	Amount     int    `json:"amount"`
}

func main() {
	dataDir := flag.String("data", "data", "path to data directory")
	flag.Parse()

	g, err := store.Load(*dataDir)
	if err != nil {
		log.Fatalf("load: %v", err)
	}

	getName := func(id string) string {
		if p := g.Persons[id]; p != nil {
			return p.FullName
		}
		return id
	}

	// Load existing relationships for cross-referencing.
	relSet := map[string]bool{}
	for _, r := range g.Relationships {
		relSet[r.PersonAID+"|"+r.PersonBID] = true
		relSet[r.PersonBID+"|"+r.PersonAID] = true
	}

	// ========================================================================
	// Type 2: Same beneficiary → hidden connections
	// ========================================================================

	// Map beneficiary_raw → list of (talent_person_id, talent_raw, commission)
	type benefEntry struct {
		talentSlug string
		talentRaw  string
		commission string
		amount     int
	}

	benefMap := map[string][]benefEntry{} // normalized beneficiary → entries

	for _, c := range g.Commissions {
		for _, grant := range c.Grants {
			if grant.BeneficiaryRaw == "" || grant.TalentPersonID == "" {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(grant.BeneficiaryRaw))
			benefMap[key] = append(benefMap[key], benefEntry{
				talentSlug: grant.TalentPersonID,
				talentRaw:  grant.TalentRaw,
				commission: c.ID,
				amount:     grant.Amount,
			})
		}
	}

	// Find beneficiaries with multiple distinct talents.
	fmt.Println("=== SAME BENEFICIARY → HIDDEN CONNECTIONS ===")
	fmt.Println()

	var sameBenefFindings []sameBeneficiary

	for benefKey, entries := range benefMap {
		// Group by unique talent.
		talentMap := map[string]*struct {
			raw    string
			grants int
			total  int
		}{}
		for _, e := range entries {
			t := talentMap[e.talentSlug]
			if t == nil {
				t = &struct {
					raw    string
					grants int
					total  int
				}{raw: e.talentRaw}
				talentMap[e.talentSlug] = t
			}
			t.grants++
			t.total += e.amount
		}

		if len(talentMap) < 2 {
			continue
		}

		// This beneficiary has multiple talents — check for jury conflicts.
		talentSlugs := make([]string, 0, len(talentMap))
		for slug := range talentMap {
			talentSlugs = append(talentSlugs, slug)
		}
		sort.Strings(talentSlugs)

		var conflicts []juryConflict

		// For each talent pair under this beneficiary, check if a jury member
		// knows one (has a relationship) and evaluated the other.
		for i, tA := range talentSlugs {
			for _, tB := range talentSlugs[i+1:] {
				// Check if any jury member has a relationship with tA and evaluated tB (or vice versa).
				for _, c := range g.Commissions {
					for _, jp := range c.Jury {
						if jp.PersonID == "" {
							continue
						}

						knowsA := relSet[jp.PersonID+"|"+tA]
						knowsB := relSet[jp.PersonID+"|"+tB]

						for _, grant := range c.Grants {
							if grant.TalentPersonID == tB && knowsA {
								conflicts = append(conflicts, juryConflict{
									JurySlug:       jp.PersonID,
									JuryName:       getName(jp.PersonID),
									KnowsTalent:    getName(tA),
									EvaluatedOther: getName(tB),
									Commission:     c.ID,
									Amount:         grant.Amount,
								})
							}
							if grant.TalentPersonID == tA && knowsB {
								conflicts = append(conflicts, juryConflict{
									JurySlug:       jp.PersonID,
									JuryName:       getName(jp.PersonID),
									KnowsTalent:    getName(tB),
									EvaluatedOther: getName(tA),
									Commission:     c.ID,
									Amount:         grant.Amount,
								})
							}
						}
					}
				}
			}
		}

		var tgs []talentGrant
		for _, slug := range talentSlugs {
			t := talentMap[slug]
			tgs = append(tgs, talentGrant{
				TalentSlug: slug,
				TalentName: getName(slug),
				TalentRaw:  t.raw,
				Grants:     t.grants,
				Total:      t.total,
			})
		}

		// Use the original case from first entry.
		displayName := entries[0].talentRaw
		for _, e := range entries {
			if e.talentRaw == strings.TrimSpace(e.talentRaw) {
				displayName = e.talentRaw
				break
			}
		}
		_ = benefKey

		sb := sameBeneficiary{
			BeneficiaryRaw: displayName,
			Talents:        tgs,
			JuryConflicts:  conflicts,
		}
		sameBenefFindings = append(sameBenefFindings, sb)
	}

	sort.Slice(sameBenefFindings, func(i, j int) bool {
		return len(sameBenefFindings[i].Talents) > len(sameBenefFindings[j].Talents)
	})

	for _, sb := range sameBenefFindings {
		fmt.Printf("Beneficiary: %s (%d talents)\n", sb.BeneficiaryRaw, len(sb.Talents))
		for _, t := range sb.Talents {
			fmt.Printf("  - %s (%s): %d grants, %d €\n", t.TalentName, t.TalentRaw, t.Grants, t.Total)
		}
		if len(sb.JuryConflicts) > 0 {
			fmt.Printf("  CONFLICTS:\n")
			for _, c := range sb.JuryConflicts {
				fmt.Printf("    %s knows %s, evaluated %s (%s, %d €)\n",
					c.JuryName, c.KnowsTalent, c.EvaluatedOther, c.Commission, c.Amount)
			}
		}
		fmt.Println()
	}

	// ========================================================================
	// Type 4: Talent funded same year they were jury
	// ========================================================================

	fmt.Println("=== JURY AND TALENT IN SAME YEAR ===")
	fmt.Println()

	// Map person → years they were jury.
	juryYears := map[string]map[string][]string{} // person → year → sessions
	for _, c := range g.Commissions {
		year := c.Date[:4]
		for _, jp := range c.Jury {
			if jp.PersonID == "" {
				continue
			}
			if juryYears[jp.PersonID] == nil {
				juryYears[jp.PersonID] = map[string][]string{}
			}
			juryYears[jp.PersonID][year] = append(juryYears[jp.PersonID][year], c.ID)
		}
	}

	// Find grants where talent_person_id was also jury in same year.
	var sameYearFindings []juryAndTalentSameYear
	seen := map[string]bool{}

	for _, c := range g.Commissions {
		year := c.Date[:4]
		for _, grant := range c.Grants {
			if grant.TalentPersonID == "" {
				continue
			}
			jurySessions := juryYears[grant.TalentPersonID][year]
			if len(jurySessions) == 0 {
				continue
			}

			key := grant.TalentPersonID + "|" + year
			if seen[key] {
				continue
			}
			seen[key] = true

			// Check they weren't jury in the SAME session (that would be truly bizarre).
			sameSession := false
			for _, js := range jurySessions {
				if js == c.ID {
					sameSession = true
				}
			}
			_ = sameSession

			// Collect all grants this person received in this year.
			var grants []grantInfo
			for _, c2 := range g.Commissions {
				if c2.Date[:4] != year {
					continue
				}
				for _, g2 := range c2.Grants {
					if g2.TalentPersonID == grant.TalentPersonID {
						grants = append(grants, grantInfo{
							Commission: c2.ID,
							Date:       c2.Date,
							TalentRaw:  g2.TalentRaw,
							Amount:     g2.Amount,
						})
					}
				}
			}

			sameYearFindings = append(sameYearFindings, juryAndTalentSameYear{
				PersonSlug:     grant.TalentPersonID,
				PersonName:     getName(grant.TalentPersonID),
				JurySessions:   jurySessions,
				GrantsSameYear: grants,
			})
		}
	}

	sort.Slice(sameYearFindings, func(i, j int) bool {
		return sameYearFindings[i].PersonSlug < sameYearFindings[j].PersonSlug
	})

	for _, f := range sameYearFindings {
		totalAmount := 0
		for _, g := range f.GrantsSameYear {
			totalAmount += g.Amount
		}
		fmt.Printf("%s — jury in %s, also received %d grants (%d €) same year\n",
			f.PersonName, strings.Join(f.JurySessions, ", "), len(f.GrantsSameYear), totalAmount)
		for _, g := range f.GrantsSameYear {
			fmt.Printf("  Grant: %s (%s) — %d €\n", g.TalentRaw, g.Date, g.Amount)
		}
		fmt.Println()
	}

	// Save all findings.
	allFindings := map[string]any{
		"same_beneficiary":          sameBenefFindings,
		"jury_and_talent_same_year": sameYearFindings,
	}
	data, _ := json.MarshalIndent(allFindings, "", "  ")
	outPath := *dataDir + "/indirect_links.json"
	os.WriteFile(outPath, data, 0o644)
	fmt.Printf("\nSaved to %s\n", outPath)
}
