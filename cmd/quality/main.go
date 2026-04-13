package main

import (
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"

	"cnc/internal/store"
)

func main() {
	dataDir := flag.String("data", "data", "path to data directory")
	flag.Parse()

	g, err := store.Load(*dataDir)
	if err != nil {
		log.Fatalf("load: %v", err)
	}

	// Collect stats.
	commissions := sortedCommissionIDs(g)
	totalGrants := 0
	totalAccepted := 0
	totalRejected := 0
	totalAmount := 0
	talentSet := map[string]bool{}
	jurySet := map[string]bool{}
	beneficiarySet := map[string]bool{}
	grantIDs := map[string][]string{} // grant ID → list of commission IDs (to detect dupes)

	var warnings []string

	for _, cid := range commissions {
		c := g.Commissions[cid]

		// Flag empty jury.
		if len(c.Jury) == 0 {
			warnings = append(warnings, fmt.Sprintf("  %s: empty jury", cid))
		}

		// Flag zero grants.
		if len(c.Grants) == 0 {
			warnings = append(warnings, fmt.Sprintf("  %s: zero grants", cid))
		}

		// Flag no président.
		hasPresident := false
		for _, jp := range c.Jury {
			if jp.Role == "président" || jp.Role == "président suppléant" {
				hasPresident = true
			}
			jurySet[strings.ToLower(jp.RawName)] = true
		}
		if !hasPresident && len(c.Jury) > 0 {
			warnings = append(warnings, fmt.Sprintf("  %s: no président in jury", cid))
		}

		for _, grant := range c.Grants {
			totalGrants++
			if grant.Result == "accepted" {
				totalAccepted++
				totalAmount += grant.Amount
			} else {
				totalRejected++
			}

			talentSet[strings.ToLower(grant.TalentRaw)] = true
			if grant.BeneficiaryRaw != "" {
				beneficiarySet[strings.ToLower(grant.BeneficiaryRaw)] = true
			}

			grantIDs[grant.ID] = append(grantIDs[grant.ID], cid)

			// Flag accepted grant with amount 0.
			if grant.Result == "accepted" && grant.Amount == 0 {
				warnings = append(warnings, fmt.Sprintf("  %s: grant %q accepted but amount=0", cid, grant.ID))
			}

			// Flag empty talent_raw.
			if grant.TalentRaw == "" {
				warnings = append(warnings, fmt.Sprintf("  %s: grant %q has empty talent_raw", cid, grant.ID))
			}
		}
	}

	// Detect duplicate grant IDs.
	for gid, cids := range grantIDs {
		if len(cids) > 1 {
			warnings = append(warnings, fmt.Sprintf("  duplicate grant ID %q in: %s", gid, strings.Join(cids, ", ")))
		}
	}

	// Print report.
	fmt.Println("=== CNC Talent Data Quality Report ===")
	fmt.Println()
	fmt.Printf("Sessions:            %d\n", len(commissions))
	fmt.Printf("  Date range:        %s → %s\n", commissions[0], commissions[len(commissions)-1])
	fmt.Println()
	fmt.Printf("Grants:              %d\n", totalGrants)
	fmt.Printf("  Accepted:          %d\n", totalAccepted)
	fmt.Printf("  Rejected:          %d\n", totalRejected)
	fmt.Printf("  Total awarded:     %s €\n", formatAmount(totalAmount))
	fmt.Println()
	fmt.Printf("Unique talent names: %d\n", len(talentSet))
	fmt.Printf("Unique jury names:   %d\n", len(jurySet))
	fmt.Printf("Unique beneficiaries:%d\n", len(beneficiarySet))
	fmt.Println()

	// Per-year breakdown.
	fmt.Println("--- Per-year breakdown ---")
	yearStats := map[string][3]int{} // year → [sessions, grants, amount]
	for _, cid := range commissions {
		c := g.Commissions[cid]
		year := c.Date[:4]
		s := yearStats[year]
		s[0]++
		for _, grant := range c.Grants {
			s[1]++
			if grant.Result == "accepted" {
				s[2] += grant.Amount
			}
		}
		yearStats[year] = s
	}
	years := sortedKeys(yearStats)
	fmt.Printf("  %-6s %8s %8s %12s\n", "Year", "Sessions", "Grants", "Awarded")
	for _, y := range years {
		s := yearStats[y]
		fmt.Printf("  %-6s %8d %8d %12s €\n", y, s[0], s[1], formatAmount(s[2]))
	}
	fmt.Println()

	// Warnings.
	if len(warnings) > 0 {
		sort.Strings(warnings)
		fmt.Printf("--- Warnings (%d) ---\n", len(warnings))
		for _, w := range warnings {
			fmt.Println(w)
		}
	} else {
		fmt.Println("--- No warnings ---")
	}
}

func sortedCommissionIDs(g *store.Graph) []string {
	ids := make([]string, 0, len(g.Commissions))
	for id := range g.Commissions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedKeys(m map[string][3]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func formatAmount(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, " ")
}
