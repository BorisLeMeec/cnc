package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"cnc/internal/model"
	"cnc/internal/store"
)

type pair struct {
	JurySlug    string
	JuryName    string
	JuryAliases []string
	CompanyRaw  string
}

type searchHit struct {
	JurySlug   string   `json:"jury_slug"`
	JuryName   string   `json:"jury_name"`
	CompanyRaw string   `json:"company_raw"`
	Results    []result `json:"results"`
}

type result struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Desc  string `json:"description"`
}

type classification struct {
	JurySlug   string `json:"jury_slug"`
	CompanyRaw string `json:"company_raw"`
	Type       string `json:"type"` // owns, employee, business, mentioned
	Evidence   string `json:"evidence"`
	Confidence string `json:"confidence"`
}

const classifyPrompt = `You are classifying relationships between people and companies based on web search result snippets.

For each pair below, you are given a jury member name, a company name, and search result snippets that mention both.

Classify each pair as ONE of:
- "owns" — the person founded, co-founded, or owns the company
- "employee" — the person works or worked at the company
- "business" — the person has a business relationship (contractor, producer, regular collaborator) but doesn't work there
- "mentioned" — they just appear together in text but there's no real professional link (interview, article mention, event)

Return ONLY a valid JSON array:
[
  {
    "jury_slug": "...",
    "company_raw": "...",
    "type": "owns" | "employee" | "business" | "mentioned",
    "evidence": "Brief explanation based on the snippets",
    "confidence": "high" | "medium" | "low"
  }
]

IMPORTANT:
- "mentioned" means NO real connection — discard these later
- Only classify as "owns"/"employee"/"business" if the snippets clearly support it
- If unsure, use "mentioned" with low confidence`

func main() {
	dataDir := flag.String("data", "data", "path to data directory")
	braveKey := flag.String("brave-key", "", "Brave Search API key")
	apiKey := flag.String("key", "", "Anthropic API key")
	flag.Parse()

	bKey := *braveKey
	if bKey == "" {
		bKey = os.Getenv("BRAVE_API_KEY")
	}
	if bKey == "" {
		log.Fatal("BRAVE_API_KEY not set")
	}

	aKey := *apiKey
	if aKey == "" {
		aKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if aKey == "" {
		log.Fatal("ANTHROPIC_API_KEY not set")
	}

	g, err := store.Load(*dataDir)
	if err != nil {
		log.Fatalf("load: %v", err)
	}

	// Load companies from talent resolution.
	companies := loadCompanies(*dataDir)
	log.Printf("Found %d companies", len(companies))

	// Build pairs.
	pairs := buildPairs(g, companies)
	log.Printf("Built %d jury×company pairs", len(pairs))

	outDir := filepath.Join(*dataDir, "enrichment", "companies")
	os.MkdirAll(outDir, 0o755)

	// Phase 1: Brave search.
	hitsFile := filepath.Join(outDir, "hits.json")
	var hits []searchHit

	if _, err := os.Stat(hitsFile); err == nil {
		// Already done — load.
		data, _ := os.ReadFile(hitsFile)
		json.Unmarshal(data, &hits)
		log.Printf("Loaded %d existing hits from cache", len(hits))
	} else {
		log.Println("=== Phase 1: Brave Search ===")
		httpClient := &http.Client{Timeout: 15 * Seconds}
		searchCount := 0

		for i, p := range pairs {
			if i%100 == 0 && i > 0 {
				log.Printf("  [%d/%d] %d searches, %d hits so far...", i, len(pairs), searchCount, len(hits))
			}

			names := uniqueNames(p.JuryName, p.JuryAliases)
			found := false
			for _, name := range names {
				if found {
					break
				}
				searchCount++
				results, err := braveSearch(httpClient, bKey, name, p.CompanyRaw)
				if err != nil {
					if strings.Contains(err.Error(), "429") {
						log.Printf("  Rate limited at search %d, stopping phase 1", searchCount)
						goto saveHits
					}
					log.Printf("  ERROR: %v", err)
					continue
				}

				// Filter results that mention both names.
				var matched []result
				nameLower := strings.ToLower(name)
				compLower := strings.ToLower(p.CompanyRaw)
				nameParts := strings.Fields(nameLower)
				for _, r := range results {
					text := strings.ToLower(r.Title + " " + r.Desc)
					nameMatch := strings.Contains(text, nameLower)
					if !nameMatch && len(nameParts) > 1 {
						last := nameParts[len(nameParts)-1]
						if len(last) > 3 {
							nameMatch = strings.Contains(text, last)
						}
					}
					compMatch := strings.Contains(text, compLower)
					if nameMatch && compMatch {
						matched = append(matched, r)
					}
				}

				if len(matched) > 0 {
					hits = append(hits, searchHit{
						JurySlug:   p.JurySlug,
						JuryName:   p.JuryName,
						CompanyRaw: p.CompanyRaw,
						Results:    matched,
					})
					found = true
					log.Printf("  HIT: %s ↔ %s (%d results)", p.JuryName, p.CompanyRaw, len(matched))
				}
			}
		}

	saveHits:
		data, _ := json.MarshalIndent(hits, "", "  ")
		os.WriteFile(hitsFile, data, 0o644)
		log.Printf("Phase 1 done: %d searches, %d hits", searchCount, len(hits))
	}

	if len(hits) == 0 {
		log.Println("No hits to classify.")
		return
	}

	// Phase 2: Haiku classification.
	classFile := filepath.Join(outDir, "classifications.json")
	if _, err := os.Stat(classFile); err == nil {
		log.Println("Classifications already exist, skipping phase 2.")
	} else {
		log.Printf("=== Phase 2: Haiku classification (%d hits) ===", len(hits))

		client := anthropic.NewClient(option.WithAPIKey(aKey))
		ctx := context.Background()

		classifications, err := classify(ctx, &client, hits)
		if err != nil {
			log.Fatalf("classify: %v", err)
		}

		// Save classifications.
		data, _ := json.MarshalIndent(classifications, "", "  ")
		os.WriteFile(classFile, data, 0o644)

		// Save relationships for non-"mentioned" ones.
		saved := 0
		for _, c := range classifications {
			if c.Type == "mentioned" {
				continue
			}
			relType := model.RelColleague
			switch c.Type {
			case "owns":
				relType = model.RelColleague // closest fit
			case "employee":
				relType = model.RelColleague
			case "business":
				relType = "business"
			}

			// Find the jury person slug — it's already in the classification
			rel := &model.Relationship{
				PersonAID:  c.JurySlug,
				PersonBID:  c.CompanyRaw, // We'll use company raw as ID for now
				Type:       relType,
				Source:     "Brave web search",
				Confidence: model.Confidence(c.Confidence),
				Notes:      fmt.Sprintf("[%s] %s", c.Type, c.Evidence),
			}
			store.SaveRelationship(*dataDir, rel)
			saved++
		}
		log.Printf("Phase 2 done: %d classified, %d saved as relationships", len(classifications), saved)
	}
}

func loadCompanies(dataDir string) []string {
	data, err := os.ReadFile(filepath.Join(dataDir, "talent_resolution.json"))
	if err != nil {
		log.Fatalf("read talent_resolution.json: %v", err)
	}
	var talents []struct {
		TalentRaw string `json:"talent_raw"`
		Type      string `json:"type"`
	}
	json.Unmarshal(data, &talents)

	var companies []string
	for _, t := range talents {
		if t.Type == "company" {
			companies = append(companies, t.TalentRaw)
		}
	}
	sort.Strings(companies)
	return companies
}

func buildPairs(g *store.Graph, companies []string) []pair {
	compSet := map[string]bool{}
	for _, c := range companies {
		compSet[strings.ToLower(c)] = true
	}

	seen := map[string]bool{}
	var pairs []pair

	for _, c := range g.Commissions {
		// Find companies in this commission's grants.
		var commCompanies []string
		for _, grant := range c.Grants {
			if compSet[strings.ToLower(grant.TalentRaw)] {
				commCompanies = append(commCompanies, grant.TalentRaw)
			}
		}
		if len(commCompanies) == 0 {
			continue
		}

		for _, jp := range c.Jury {
			if jp.PersonID == "" {
				continue
			}
			juryPerson := g.Persons[jp.PersonID]
			juryName := jp.RawName
			var aliases []string
			if juryPerson != nil {
				juryName = juryPerson.FullName
				aliases = juryPerson.Aliases
			}

			for _, comp := range commCompanies {
				key := jp.PersonID + "|" + comp
				if seen[key] {
					continue
				}
				seen[key] = true
				pairs = append(pairs, pair{
					JurySlug:    jp.PersonID,
					JuryName:    juryName,
					JuryAliases: aliases,
					CompanyRaw:  comp,
				})
			}
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].JurySlug != pairs[j].JurySlug {
			return pairs[i].JurySlug < pairs[j].JurySlug
		}
		return pairs[i].CompanyRaw < pairs[j].CompanyRaw
	})
	return pairs
}

func braveSearch(client *http.Client, apiKey, name, company string) ([]result, error) {
	query := fmt.Sprintf(`"%s" "%s"`, name, company)
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

	var braveResp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &braveResp); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	var results []result
	for _, r := range braveResp.Web.Results {
		results = append(results, result{
			Title: r.Title,
			URL:   r.URL,
			Desc:  r.Description,
		})
	}
	return results, nil
}

func classify(ctx context.Context, client *anthropic.Client, hits []searchHit) ([]classification, error) {
	var b strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&b, "### Pair %d: %s ↔ %s\n", i+1, h.JuryName, h.CompanyRaw)
		fmt.Fprintf(&b, "Jury slug: %s\n", h.JurySlug)
		for j, r := range h.Results {
			fmt.Fprintf(&b, "  Result %d: %s\n", j+1, r.Title)
			fmt.Fprintf(&b, "    URL: %s\n", r.URL)
			fmt.Fprintf(&b, "    Snippet: %s\n", r.Desc)
		}
		fmt.Fprintln(&b)
	}

	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 8192,
		System: []anthropic.TextBlockParam{
			{Text: classifyPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(b.String())),
		},
	})
	defer stream.Close()

	var msg anthropic.Message
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			return nil, fmt.Errorf("accumulate: %w", err)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}

	raw := ""
	for _, block := range msg.Content {
		if block.Type == "text" {
			raw = block.Text
			break
		}
	}
	raw = strings.TrimSpace(raw)
	raw = stripCodeFence(raw)

	var classifications []classification
	if err := json.Unmarshal([]byte(raw), &classifications); err != nil {
		return nil, fmt.Errorf("parse: %w\nraw: %s", err, raw)
	}
	return classifications, nil
}

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

func stripCodeFence(s string) string {
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			inner := s[idx+1:]
			if last := strings.LastIndex(inner, "```"); last != -1 {
				inner = inner[:last]
			}
			return strings.TrimSpace(inner)
		}
	}
	return s
}

var _ = regexp.Compile // keep import

// Seconds is a time.Duration alias for readability.
const Seconds = 1e9
