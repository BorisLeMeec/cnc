package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"cnc/internal/model"
	"cnc/internal/store"
)

// talentEntry is the input context we give to Claude for each unique talent.
type talentEntry struct {
	TalentRaw     string   `json:"talent_raw"`
	Beneficiaries []string `json:"beneficiaries"` // associated beneficiary_raw values
	GrantCount    int      `json:"grant_count"`
}

// talentResult is what Claude returns for each talent.
type talentResult struct {
	TalentRaw  string `json:"talent_raw"`
	Type       string `json:"type"` // "person" or "company"
	RealName   string `json:"real_name,omitempty"`
	PersonSlug string `json:"person_slug,omitempty"`
	Confidence string `json:"confidence"` // "high", "medium", "low"
	Notes      string `json:"notes,omitempty"`
}

// juryGroup is what Claude returns for each deduplicated jury person.
type juryGroup struct {
	CanonicalName string   `json:"canonical_name"`
	Slug          string   `json:"slug"`
	RawVariants   []string `json:"raw_variants"`
}

const juryPrompt = `You are helping deduplicate jury member names from French CNC commission data.

Below is a list of jury member raw names exactly as they appear on official CNC pages.
Some names may refer to the same person with different capitalization, accents, or minor variations.

Group them into unique persons. For each person provide:
- "canonical_name": the best-formatted full name (proper case, accents)
- "slug": kebab-case ID (e.g. "benjamin-bonnet")
- "raw_variants": array of all raw name strings that refer to this person

Return ONLY a valid JSON array, no markdown, no commentary.`

const talentPrompt = `You are helping classify and resolve talent/creator names from French CNC (Centre National du Cinéma) "CNC Talent" fund data.

This fund supports video creators on the internet (YouTubers, streamers, etc.). Each entry below has:
- "talent_raw": the name as it appears on the CNC commission page
- "beneficiaries": the legal entities that received the money for this talent's projects
- "grant_count": how many times this talent appears across all commissions

For each entry, determine:
1. "type": Is this a "person" (individual creator) or "company" (media company, production house, collective that is NOT a single person)?
   - Most entries are persons (individual video creators)
   - A talent is a "company" only if it's clearly an organization/media entity, NOT just a business name wrapping a solo creator
   - If the talent_raw looks like a personal pseudonym/channel name, it's a "person" even if the beneficiary is a company

2. "real_name": If type is "person", provide the real full name of the creator.
   - If talent_raw already IS a real name, use it (properly formatted)
   - If talent_raw is a pseudonym/channel name, try to identify the real person behind it
   - If you cannot determine the real name, use the talent_raw as-is

3. "person_slug": kebab-case slug of the real name (e.g. "benjamin-brillaud")

4. "confidence": "high" if you're sure, "medium" if reasonable guess, "low" if uncertain

5. "notes": optional, brief note if relevant (e.g. "YouTube channel name" or "production company")

Return ONLY a valid JSON array of objects with fields: talent_raw, type, real_name, person_slug, confidence, notes.
No markdown, no commentary.`

func main() {
	dataDir := flag.String("data", "data", "path to data directory")
	apiKey := flag.String("key", "", "Anthropic API key")
	flag.Parse()

	key := *apiKey
	if key == "" {
		key = os.Getenv("ANTHROPIC_API_KEY")
	}
	if key == "" {
		log.Fatal("ANTHROPIC_API_KEY not set")
	}

	g, err := store.Load(*dataDir)
	if err != nil {
		log.Fatalf("load: %v", err)
	}

	client := anthropic.NewClient(option.WithAPIKey(key))
	ctx := context.Background()

	// 1. Collect unique jury names.
	juryNames := collectJuryNames(g)
	log.Printf("Found %d unique jury raw names", len(juryNames))

	// 2. Collect unique talent info with context.
	talents := collectTalentInfo(g)
	log.Printf("Found %d unique talent_raw values", len(talents))

	// 3. Resolve jury names via AI.
	log.Println("Resolving jury names...")
	juryGroups, err := resolveJury(ctx, &client, juryNames)
	if err != nil {
		log.Fatalf("resolve jury: %v", err)
	}
	log.Printf("Jury: %d raw names → %d unique persons", len(juryNames), len(juryGroups))

	// 4. Resolve talents via AI in batches.
	log.Println("Resolving talent names...")
	talentResults, err := resolveTalents(ctx, &client, talents)
	if err != nil {
		log.Fatalf("resolve talents: %v", err)
	}

	personCount, companyCount := 0, 0
	for _, r := range talentResults {
		if r.Type == "person" {
			personCount++
		} else {
			companyCount++
		}
	}
	log.Printf("Talents: %d persons, %d companies", personCount, companyCount)

	// 5. Build and save Person records (merge jury + talent persons).
	persons := buildPersons(juryGroups, talentResults)
	log.Printf("Total unique persons: %d", len(persons))

	for _, p := range persons {
		if err := store.SavePerson(*dataDir, p); err != nil {
			log.Printf("ERROR saving person %s: %v", p.ID, err)
		}
	}
	log.Printf("Saved %d person records to %s/persons/", len(persons), *dataDir)

	// 6. Save talent resolution mapping.
	resPath := saveTalentResolution(*dataDir, talentResults)
	log.Printf("Saved talent resolution to %s", resPath)

	// 7. Save jury resolution mapping.
	juryPath := saveJuryResolution(*dataDir, juryGroups)
	log.Printf("Saved jury resolution to %s", juryPath)
}

// collectJuryNames returns sorted unique jury raw names.
func collectJuryNames(g *store.Graph) []string {
	seen := map[string]bool{}
	for _, c := range g.Commissions {
		for _, jp := range c.Jury {
			seen[jp.RawName] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// collectTalentInfo returns unique talent entries with beneficiary context.
func collectTalentInfo(g *store.Graph) []talentEntry {
	type info struct {
		beneficiaries map[string]bool
		count         int
	}
	m := map[string]*info{}

	for _, c := range g.Commissions {
		for _, grant := range c.Grants {
			key := grant.TalentRaw
			if key == "" {
				continue
			}
			if m[key] == nil {
				m[key] = &info{beneficiaries: map[string]bool{}}
			}
			m[key].count++
			if grant.BeneficiaryRaw != "" {
				m[key].beneficiaries[grant.BeneficiaryRaw] = true
			}
		}
	}

	entries := make([]talentEntry, 0, len(m))
	for raw, inf := range m {
		bens := make([]string, 0, len(inf.beneficiaries))
		for b := range inf.beneficiaries {
			bens = append(bens, b)
		}
		sort.Strings(bens)
		entries = append(entries, talentEntry{
			TalentRaw:     raw,
			Beneficiaries: bens,
			GrantCount:    inf.count,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].TalentRaw < entries[j].TalentRaw
	})
	return entries
}

// resolveJury sends all jury names to Claude and returns deduplicated groups.
func resolveJury(ctx context.Context, client *anthropic.Client, names []string) ([]juryGroup, error) {
	input, _ := json.MarshalIndent(names, "", "  ")
	raw, err := callClaude(ctx, client, juryPrompt, string(input))
	if err != nil {
		return nil, err
	}
	var groups []juryGroup
	if err := json.Unmarshal([]byte(raw), &groups); err != nil {
		return nil, fmt.Errorf("parse jury response: %w\n%s", err, raw)
	}
	return groups, nil
}

// resolveTalents sends talent entries in batches to Claude.
func resolveTalents(ctx context.Context, client *anthropic.Client, talents []talentEntry) ([]talentResult, error) {
	const batchSize = 100
	var all []talentResult

	for i := 0; i < len(talents); i += batchSize {
		end := i + batchSize
		if end > len(talents) {
			end = len(talents)
		}
		batch := talents[i:end]
		log.Printf("  Talent batch %d-%d of %d...", i+1, end, len(talents))

		input, _ := json.MarshalIndent(batch, "", "  ")
		raw, err := callClaude(ctx, client, talentPrompt, string(input))
		if err != nil {
			return nil, fmt.Errorf("batch %d-%d: %w", i+1, end, err)
		}

		var results []talentResult
		if err := json.Unmarshal([]byte(raw), &results); err != nil {
			return nil, fmt.Errorf("parse talent batch %d-%d: %w\n%s", i+1, end, err, raw)
		}
		all = append(all, results...)
	}
	return all, nil
}

// callClaude sends a prompt to Claude Sonnet and returns the text response.
func callClaude(ctx context.Context, client *anthropic.Client, system, user string) (string, error) {
	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 32768,
		System: []anthropic.TextBlockParam{
			{Text: system},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	defer stream.Close()

	var msg anthropic.Message
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			return "", fmt.Errorf("accumulate: %w", err)
		}
	}
	if err := stream.Err(); err != nil {
		return "", err
	}

	for _, block := range msg.Content {
		if block.Type == "text" {
			text := strings.TrimSpace(block.Text)
			// Strip code fences if present.
			if strings.HasPrefix(text, "```") {
				if idx := strings.Index(text, "\n"); idx != -1 {
					text = text[idx+1:]
				}
				if last := strings.LastIndex(text, "```"); last != -1 {
					text = text[:last]
				}
				text = strings.TrimSpace(text)
			}
			return text, nil
		}
	}
	return "", fmt.Errorf("no text in response")
}

// buildPersons merges jury and talent resolution into Person records.
func buildPersons(jury []juryGroup, talents []talentResult) []*model.Person {
	personsBySlug := map[string]*model.Person{}

	// Add jury persons.
	for _, jg := range jury {
		slug := jg.Slug
		p := personsBySlug[slug]
		if p == nil {
			p = &model.Person{
				ID:       slug,
				FullName: jg.CanonicalName,
			}
			personsBySlug[slug] = p
		}
		for _, v := range jg.RawVariants {
			if !contains(p.Aliases, v) && v != p.FullName {
				p.Aliases = append(p.Aliases, v)
			}
		}
	}

	// Add talent persons (skip companies).
	for _, tr := range talents {
		if tr.Type != "person" || tr.PersonSlug == "" {
			continue
		}
		slug := tr.PersonSlug
		p := personsBySlug[slug]
		if p == nil {
			p = &model.Person{
				ID:       slug,
				FullName: tr.RealName,
			}
			personsBySlug[slug] = p
		}
		// Add talent_raw as alias if different from full name.
		if tr.TalentRaw != p.FullName && !contains(p.Aliases, tr.TalentRaw) {
			p.Aliases = append(p.Aliases, tr.TalentRaw)
		}
		if tr.Notes != "" && p.Notes == "" {
			p.Notes = tr.Notes
		}
	}

	// Sort aliases for deterministic output.
	persons := make([]*model.Person, 0, len(personsBySlug))
	for _, p := range personsBySlug {
		sort.Strings(p.Aliases)
		persons = append(persons, p)
	}
	sort.Slice(persons, func(i, j int) bool {
		return persons[i].ID < persons[j].ID
	})
	return persons
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func saveTalentResolution(dataDir string, results []talentResult) string {
	path := dataDir + "/talent_resolution.json"
	data, _ := json.MarshalIndent(results, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("ERROR writing %s: %v", path, err)
	}
	return path
}

func saveJuryResolution(dataDir string, groups []juryGroup) string {
	path := dataDir + "/jury_resolution.json"
	data, _ := json.MarshalIndent(groups, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("ERROR writing %s: %v", path, err)
	}
	return path
}
