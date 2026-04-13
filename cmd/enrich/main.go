package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cnc/internal/model"
	"cnc/internal/store"
)

// Jury members known to be creators/YouTubers.
var creatorJuryMembers = map[string]bool{
	"cyprien-iov":          true,
	"hugo-travers":         true,
	"patrick-baud":         true,
	"mathieu-guyan":        true,
	"charlie-danger":       true,
	"marion-seclin":        true,
	"florent-bernard":      true,
	"julien-josselin":      true,
	"ina-mihalache":        true,
	"lea-bordier":          true,
	"kevin-tran":           true,
	"romain-filstroff":     true,
	"timothee-hochet":      true,
	"sophie-marie-larrouy": true,
	"shirley-souagnon":     true,
	"aude-gogny-goubert":   true,
	"manon-champier":       true,
	"ambroise-carminati":   true,
	"jeanne-seignol":       true,
	"maurice-barthelemy":   true,
	"victor-habchy":        true,
	"anais-volpe":          true,
	"nora-hamzawi":         true,
	"fif-tobossi":          true,
}

type pair struct {
	JurySlug      string
	JuryName      string
	JuryAliases   []string // channel names, pseudonyms
	TalentSlug    string
	TalentName    string
	TalentAliases []string // talent_raw, channel names
}

type braveResult struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
	Videos struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"videos"`
}

type connection struct {
	JurySlug   string  `json:"jury_slug"`
	JuryName   string  `json:"jury_name"`
	TalentSlug string  `json:"talent_slug"`
	TalentName string  `json:"talent_name"`
	Matches    []match `json:"matches"`
}

type match struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Desc  string `json:"description"`
}

func main() {
	dataDir := flag.String("data", "data", "path to data directory")
	braveKey := flag.String("brave-key", "", "Brave Search API key")
	flag.Parse()

	key := *braveKey
	if key == "" {
		key = os.Getenv("BRAVE_API_KEY")
	}
	if key == "" {
		log.Fatal("BRAVE_API_KEY not set")
	}

	g, err := store.Load(*dataDir)
	if err != nil {
		log.Fatalf("load: %v", err)
	}

	pairs := buildPairs(g)
	log.Printf("Built %d pairs to search (from %d creator-jury members)", len(pairs), len(creatorJuryMembers))

	outDir := filepath.Join(*dataDir, "enrichment", "youtube")
	os.MkdirAll(outDir, 0o755)

	// Load already-matched pairs (skip those — already found).
	matchedFile := filepath.Join(outDir, "matched.json")
	matched := loadProgress(matchedFile)

	// Load already-processed pairs for resumability.
	doneFile := filepath.Join(outDir, "progress.json")
	done := loadProgress(doneFile)
	log.Printf("Already processed: %d pairs (%d matched)", len(done), len(matched))

	httpClient := &http.Client{Timeout: 15 * time.Second}
	var allConnections []connection

	for i, p := range pairs {
		pairKey := p.JurySlug + "|" + p.TalentSlug
		if matched[pairKey] {
			continue
		}

		// If already searched with primary names, only try alias combos.
		skipPrimary := done[pairKey]

		// Skip entirely if no aliases to try and already done.
		if skipPrimary && len(p.JuryAliases) == 0 && len(p.TalentAliases) == 0 {
			continue
		}

		log.Printf("[%d/%d] %s ↔ %s", i+1, len(pairs), p.JuryName, p.TalentName)

		matches, err := searchBrave(httpClient, key, p, skipPrimary)
		if err != nil {
			log.Printf("  ERROR: %v", err)
			// Rate limit — wait and retry once.
			if strings.Contains(err.Error(), "429") {
				log.Printf("  Rate limited, waiting 60s...")
				time.Sleep(60 * time.Second)
				matches, err = searchBrave(httpClient, key, p, skipPrimary)
				if err != nil {
					log.Printf("  ERROR (retry): %v", err)
					continue
				}
			} else {
				continue
			}
		}

		if len(matches) > 0 {
			conn := connection{
				JurySlug:   p.JurySlug,
				JuryName:   p.JuryName,
				TalentSlug: p.TalentSlug,
				TalentName: p.TalentName,
				Matches:    matches,
			}
			allConnections = append(allConnections, conn)
			log.Printf("  MATCH! %d results with both names", len(matches))

			// Save relationship file.
			rel := &model.Relationship{
				PersonAID:  sortFirst(p.JurySlug, p.TalentSlug),
				PersonBID:  sortSecond(p.JurySlug, p.TalentSlug),
				Type:       model.RelWorkedTogether,
				Source:     matches[0].URL,
				Confidence: model.ConfidenceHigh,
				Notes:      fmt.Sprintf("YouTube co-occurrence: %s", matches[0].Title),
			}
			store.SaveRelationship(*dataDir, rel)
			matched[pairKey] = true
			saveProgress(matchedFile, matched)
		}

		// Mark as done.
		done[pairKey] = true

		// Save progress every 50 pairs.
		if len(done)%50 == 0 {
			saveProgress(doneFile, done)
		}

	}

	// Final save.
	saveProgress(doneFile, done)

	// Save all connections.
	connFile := filepath.Join(outDir, "connections.json")
	data, _ := json.MarshalIndent(allConnections, "", "  ")
	os.WriteFile(connFile, data, 0o644)

	log.Printf("Done! Found %d connections out of %d pairs searched", len(allConnections), len(pairs))
}

// searchBrave queries Brave Search for co-occurrence of two names on YouTube.
// If skipPrimary is true, skips the primary×primary combo (already searched).
func searchBrave(client *http.Client, apiKey string, p pair, skipPrimary bool) ([]match, error) {
	// Build all name variants to search.
	juryNames := uniqueNames(p.JuryName, p.JuryAliases)
	talentNames := uniqueNames(p.TalentName, p.TalentAliases)

	// Try combinations — stop at first match.
	for i, jn := range juryNames {
		for j, tn := range talentNames {
			// Skip primary×primary if already searched.
			if skipPrimary && i == 0 && j == 0 {
				continue
			}
			matches, err := braveQuery(client, apiKey, jn, tn)
			if err != nil {
				return nil, err
			}
			if len(matches) > 0 {
				return matches, nil
			}
		}
	}
	return nil, nil
}

// uniqueNames returns deduplicated, non-empty name variants (primary first).
func uniqueNames(primary string, aliases []string) []string {
	seen := map[string]bool{}
	var names []string
	for _, n := range append([]string{primary}, aliases...) {
		lower := strings.ToLower(strings.TrimSpace(n))
		if lower == "" || seen[lower] {
			continue
		}
		seen[lower] = true
		names = append(names, n)
	}
	return names
}

// braveQuery does a single Brave Search API call for two names on YouTube.
func braveQuery(client *http.Client, apiKey, nameA, nameB string) ([]match, error) {
	query := fmt.Sprintf(`"%s" "%s" site:youtube.com`, nameA, nameB)

	u := "https://api.search.brave.com/res/v1/web/search?" + url.Values{
		"q":     {query},
		"count": {"10"},
	}.Encode()

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("X-Subscription-Token", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("gzip: %w", err)
		}
		defer gz.Close()
		reader = gz
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	var result braveResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	// Check which results mention BOTH names.
	aLower := strings.ToLower(nameA)
	bLower := strings.ToLower(nameB)
	aParts := nameParts(nameA)
	bParts := nameParts(nameB)

	var matches []match
	for _, r := range result.Web.Results {
		text := strings.ToLower(r.Title + " " + r.Description)
		if containsName(text, aLower, aParts) && containsName(text, bLower, bParts) {
			matches = append(matches, match{
				Title: r.Title,
				URL:   r.URL,
				Desc:  r.Description,
			})
		}
	}

	return matches, nil
}

// containsName checks if text contains the full name or at least the last name.
func containsName(text, fullName string, parts []string) bool {
	if strings.Contains(text, fullName) {
		return true
	}
	// Check if last name appears (most distinctive part).
	if len(parts) > 1 {
		lastName := parts[len(parts)-1]
		if len(lastName) > 3 && strings.Contains(text, lastName) {
			return true
		}
	}
	return false
}

// nameParts splits a name into lowercase parts.
func nameParts(name string) []string {
	parts := strings.Fields(strings.ToLower(name))
	return parts
}

func buildPairs(g *store.Graph) []pair {
	seen := map[string]bool{}
	var pairs []pair

	for _, c := range g.Commissions {
		for _, jp := range c.Jury {
			if jp.PersonID == "" || !creatorJuryMembers[jp.PersonID] {
				continue
			}
			juryPerson := g.Persons[jp.PersonID]
			juryName := jp.RawName
			if juryPerson != nil {
				juryName = juryPerson.FullName
			}

			for _, grant := range c.Grants {
				if grant.TalentPersonID == "" {
					continue
				}
				key := jp.PersonID + "|" + grant.TalentPersonID
				if seen[key] {
					continue
				}
				seen[key] = true

				talentPerson := g.Persons[grant.TalentPersonID]
				talentName := grant.TalentRaw
				var talentAliases []string
				if talentPerson != nil {
					talentName = talentPerson.FullName
					// Add talent_raw as alias if different from full name.
					if grant.TalentRaw != talentPerson.FullName {
						talentAliases = append(talentAliases, grant.TalentRaw)
					}
					talentAliases = append(talentAliases, talentPerson.Aliases...)
				}

				var juryAliases []string
				if juryPerson != nil {
					juryAliases = juryPerson.Aliases
				}

				pairs = append(pairs, pair{
					JurySlug:      jp.PersonID,
					JuryName:      juryName,
					JuryAliases:   juryAliases,
					TalentSlug:    grant.TalentPersonID,
					TalentName:    talentName,
					TalentAliases: talentAliases,
				})
			}
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].JurySlug != pairs[j].JurySlug {
			return pairs[i].JurySlug < pairs[j].JurySlug
		}
		return pairs[i].TalentSlug < pairs[j].TalentSlug
	})
	return pairs
}

func loadProgress(path string) map[string]bool {
	m := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	var keys []string
	json.Unmarshal(data, &keys)
	for _, k := range keys {
		m[k] = true
	}
	return m
}

func saveProgress(path string, done map[string]bool) {
	keys := make([]string, 0, len(done))
	for k := range done {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	data, _ := json.Marshal(keys)
	os.WriteFile(path, data, 0o644)
}

func sortFirst(a, b string) string {
	if a < b {
		return a
	}
	return b
}

func sortSecond(a, b string) string {
	if a < b {
		return b
	}
	return a
}
