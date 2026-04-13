package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"cnc/internal/store"
)

type finding struct {
	Type        string   `json:"type"` // "co_jury_then_evaluated", "reciprocal_funding"
	PersonA     string   `json:"person_a"`
	PersonAName string   `json:"person_a_name"`
	PersonB     string   `json:"person_b"`
	PersonBName string   `json:"person_b_name"`
	Details     []detail `json:"details"`
}

type detail struct {
	Commission string `json:"commission"`
	Date       string `json:"date"`
	Role       string `json:"role"` // "co_jury", "a_evaluated_b", "b_evaluated_a"
	Amount     int    `json:"amount,omitempty"`
}

func main() {
	dataDir := flag.String("data", "data", "path to data directory")
	flag.Parse()

	g, err := store.Load(*dataDir)
	if err != nil {
		log.Fatalf("load: %v", err)
	}

	// Sort commissions by date.
	type comm struct {
		id   string
		date string
	}
	var comms []comm
	for _, c := range g.Commissions {
		comms = append(comms, comm{c.ID, c.Date})
	}
	sort.Slice(comms, func(i, j int) bool { return comms[i].date < comms[j].date })

	// Build indexes:
	// 1. Who was jury together in which session
	// 2. Who evaluated whom (jury → talent) with amount

	type evalRecord struct {
		commission string
		date       string
		amount     int
	}

	// coJury[A][B] = list of sessions where A and B were both jury
	coJury := map[string]map[string][]comm{}
	// evaluated[juryID][talentID] = list of eval records
	evaluated := map[string]map[string][]evalRecord{}

	for _, c := range g.Commissions {
		juryIDs := []string{}
		for _, jp := range c.Jury {
			if jp.PersonID != "" {
				juryIDs = append(juryIDs, jp.PersonID)
			}
		}

		// Co-jury pairs.
		for i, a := range juryIDs {
			for j, b := range juryIDs {
				if i >= j {
					continue
				}
				if coJury[a] == nil {
					coJury[a] = map[string][]comm{}
				}
				if coJury[b] == nil {
					coJury[b] = map[string][]comm{}
				}
				coJury[a][b] = append(coJury[a][b], comm{c.ID, c.Date})
				coJury[b][a] = append(coJury[b][a], comm{c.ID, c.Date})
			}
		}

		// Evaluated pairs.
		for _, jp := range c.Jury {
			if jp.PersonID == "" {
				continue
			}
			for _, grant := range c.Grants {
				if grant.TalentPersonID == "" {
					continue
				}
				if evaluated[jp.PersonID] == nil {
					evaluated[jp.PersonID] = map[string][]evalRecord{}
				}
				evaluated[jp.PersonID][grant.TalentPersonID] = append(
					evaluated[jp.PersonID][grant.TalentPersonID],
					evalRecord{c.ID, c.Date, grant.Amount},
				)
			}
		}
	}

	getName := func(id string) string {
		if p := g.Persons[id]; p != nil {
			return p.FullName
		}
		return id
	}

	var findings []finding

	// --- Type 1: Co-jury then evaluated ---
	// A and B were on the jury together, then later A evaluates B's grant (or vice versa).
	seen1 := map[string]bool{}
	for juryID, talents := range evaluated {
		for talentID, evals := range talents {
			key := juryID + "|" + talentID
			if seen1[key] {
				continue
			}

			coSessions := coJury[juryID][talentID]
			if len(coSessions) == 0 {
				continue
			}

			seen1[key] = true

			var details []detail
			for _, cs := range coSessions {
				details = append(details, detail{
					Commission: cs.id,
					Date:       cs.date,
					Role:       "co_jury",
				})
			}
			for _, ev := range evals {
				details = append(details, detail{
					Commission: ev.commission,
					Date:       ev.date,
					Role:       "a_evaluated_b",
					Amount:     ev.amount,
				})
			}

			sort.Slice(details, func(i, j int) bool {
				return details[i].Date < details[j].Date
			})

			findings = append(findings, finding{
				Type:        "co_jury_then_evaluated",
				PersonA:     juryID,
				PersonAName: getName(juryID),
				PersonB:     talentID,
				PersonBName: getName(talentID),
				Details:     details,
			})
		}
	}

	// --- Type 2: Reciprocal funding ---
	// A evaluated B AND B evaluated A.
	seen2 := map[string]bool{}
	for a, talents := range evaluated {
		for b, aEvalsB := range talents {
			pairKey := min(a, b) + "|" + max(a, b)
			if seen2[pairKey] {
				continue
			}

			bEvalsA := evaluated[b][a]
			if len(bEvalsA) == 0 {
				continue
			}

			seen2[pairKey] = true

			var details []detail
			for _, ev := range aEvalsB {
				details = append(details, detail{
					Commission: ev.commission,
					Date:       ev.date,
					Role:       "a_evaluated_b",
					Amount:     ev.amount,
				})
			}
			for _, ev := range bEvalsA {
				details = append(details, detail{
					Commission: ev.commission,
					Date:       ev.date,
					Role:       "b_evaluated_a",
					Amount:     ev.amount,
				})
			}

			sort.Slice(details, func(i, j int) bool {
				return details[i].Date < details[j].Date
			})

			findings = append(findings, finding{
				Type:        "reciprocal_funding",
				PersonA:     min(a, b),
				PersonAName: getName(min(a, b)),
				PersonB:     max(a, b),
				PersonBName: getName(max(a, b)),
				Details:     details,
			})
		}
	}

	// Sort findings.
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Type != findings[j].Type {
			return findings[i].Type < findings[j].Type
		}
		return findings[i].PersonA < findings[j].PersonA
	})

	// Print summary.
	coJuryCount := 0
	reciprocalCount := 0
	for _, f := range findings {
		if f.Type == "co_jury_then_evaluated" {
			coJuryCount++
		} else {
			reciprocalCount++
		}
	}

	fmt.Printf("=== Cross-session links ===\n\n")
	fmt.Printf("Co-jury then evaluated: %d\n", coJuryCount)
	fmt.Printf("Reciprocal funding:     %d\n\n", reciprocalCount)

	// Print reciprocal funding (most interesting).
	fmt.Println("--- RECIPROCAL FUNDING ---")
	for _, f := range findings {
		if f.Type != "reciprocal_funding" {
			continue
		}
		fmt.Printf("\n%s ↔ %s\n", f.PersonAName, f.PersonBName)
		for _, d := range f.Details {
			arrow := ""
			switch d.Role {
			case "a_evaluated_b":
				arrow = fmt.Sprintf("  %s: %s gave %s %d €", d.Date, f.PersonAName, f.PersonBName, d.Amount)
			case "b_evaluated_a":
				arrow = fmt.Sprintf("  %s: %s gave %s %d €", d.Date, f.PersonBName, f.PersonAName, d.Amount)
			}
			fmt.Println(arrow)
		}
	}

	fmt.Println("\n--- CO-JURY THEN EVALUATED (sample) ---")
	count := 0
	for _, f := range findings {
		if f.Type != "co_jury_then_evaluated" {
			continue
		}
		if count >= 20 {
			fmt.Printf("... and %d more\n", coJuryCount-20)
			break
		}
		coCount := 0
		evalCount := 0
		totalAmount := 0
		for _, d := range f.Details {
			if d.Role == "co_jury" {
				coCount++
			} else {
				evalCount++
				totalAmount += d.Amount
			}
		}
		fmt.Printf("  %s was co-jury with %s (%d times), then evaluated their grants (%d grants, %d €)\n",
			f.PersonAName, f.PersonBName, coCount, evalCount, totalAmount)
		count++
	}

	// Save to file.
	data, _ := json.MarshalIndent(findings, "", "  ")
	outPath := *dataDir + "/cross_links.json"
	os.WriteFile(outPath, data, 0o644)
	fmt.Printf("\nSaved %d findings to %s\n", len(findings), outPath)
}

func min(a, b string) string {
	if a < b {
		return a
	}
	return b
}

func max(a, b string) string {
	if a < b {
		return b
	}
	return a
}
