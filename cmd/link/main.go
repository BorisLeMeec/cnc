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

type juryGroup struct {
	CanonicalName string   `json:"canonical_name"`
	Slug          string   `json:"slug"`
	RawVariants   []string `json:"raw_variants"`
}

type talentResult struct {
	TalentRaw  string `json:"talent_raw"`
	Type       string `json:"type"`
	PersonSlug string `json:"person_slug,omitempty"`
}

func main() {
	dataDir := flag.String("data", "data", "path to data directory")
	flag.Parse()

	// Load resolution mappings.
	juryMap, err := loadJuryMap(*dataDir)
	if err != nil {
		log.Fatalf("load jury resolution: %v", err)
	}
	log.Printf("Jury mapping: %d raw names → person slugs", len(juryMap))

	talentMap, err := loadTalentMap(*dataDir)
	if err != nil {
		log.Fatalf("load talent resolution: %v", err)
	}
	log.Printf("Talent mapping: %d talent_raw → person slugs (persons only)", len(talentMap))

	// Load graph.
	g, err := store.Load(*dataDir)
	if err != nil {
		log.Fatalf("load: %v", err)
	}

	// Link and save each commission.
	commissions := sortedIDs(g)
	juryLinked, talentLinked := 0, 0
	juryMissing, talentMissing := map[string]bool{}, map[string]bool{}

	for _, cid := range commissions {
		c := g.Commissions[cid]
		changed := false

		// Link jury.
		for i := range c.Jury {
			jp := &c.Jury[i]
			if slug, ok := juryMap[jp.RawName]; ok {
				if jp.PersonID != slug {
					jp.PersonID = slug
					changed = true
					juryLinked++
				}
			} else {
				juryMissing[jp.RawName] = true
			}
		}

		// Link talents.
		for i := range c.Grants {
			grant := &c.Grants[i]
			if slug, ok := talentMap[grant.TalentRaw]; ok {
				if grant.TalentPersonID != slug {
					grant.TalentPersonID = slug
					changed = true
					talentLinked++
				}
			} else if grant.TalentRaw != "" {
				talentMissing[grant.TalentRaw] = true
			}
		}

		if changed {
			if err := store.SaveCommission(*dataDir, c); err != nil {
				log.Printf("ERROR saving %s: %v", cid, err)
			}
		}
	}

	log.Printf("Linked %d jury entries, %d talent entries across %d commissions", juryLinked, talentLinked, len(commissions))

	if len(juryMissing) > 0 {
		names := sortedSet(juryMissing)
		log.Printf("WARNING: %d jury raw names not in resolution:", len(names))
		for _, n := range names {
			fmt.Printf("  jury: %q\n", n)
		}
	}

	if len(talentMissing) > 0 {
		names := sortedSet(talentMissing)
		log.Printf("WARNING: %d talent_raw values not in resolution:", len(names))
		for _, n := range names {
			fmt.Printf("  talent: %q\n", n)
		}
	}
}

// loadJuryMap builds raw_name → slug from jury_resolution.json.
func loadJuryMap(dataDir string) (map[string]string, error) {
	data, err := os.ReadFile(dataDir + "/jury_resolution.json")
	if err != nil {
		return nil, err
	}
	var groups []juryGroup
	if err := json.Unmarshal(data, &groups); err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, g := range groups {
		for _, v := range g.RawVariants {
			m[v] = g.Slug
		}
	}
	return m, nil
}

// loadTalentMap builds talent_raw → person_slug from talent_resolution.json (persons only).
func loadTalentMap(dataDir string) (map[string]string, error) {
	data, err := os.ReadFile(dataDir + "/talent_resolution.json")
	if err != nil {
		return nil, err
	}
	var results []talentResult
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, r := range results {
		if r.Type == "person" && r.PersonSlug != "" {
			m[r.TalentRaw] = r.PersonSlug
		}
	}
	return m, nil
}

func sortedIDs(g *store.Graph) []string {
	ids := make([]string, 0, len(g.Commissions))
	for id := range g.Commissions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedSet(m map[string]bool) []string {
	s := make([]string, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	return s
}

// Ensure strings import is used.
var _ = strings.ToLower
