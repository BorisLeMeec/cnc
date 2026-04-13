package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"cnc/internal/model"
	"cnc/internal/store"
)

const systemPrompt = `You are a data extraction assistant for the French CNC (Centre National du Cinéma).
You extract structured data from CNC commission result pages for the "Fonds d'aide aux créateurs vidéo sur internet (CNC Talent)" fund.

You must return ONLY a valid JSON array of commission objects — no markdown, no commentary, no code fences.

Each commission object must follow this exact structure:
{
  "id": "cnc-talent-YYYY-MM-DD",
  "fund_name": "CNC Talent",
  "date": "YYYY-MM-DD",
  "source_url": "<the URL of the page>",
  "jury": [
    {
      "raw_name": "Exact name as written on page",
      "role": "président" | "président suppléant" | "membre",
      "person_id": ""
    }
  ],
  "grants": [
    {
      "id": "cnc-talent-YYYY-MM-DD-<short-slug>",
      "project_id": "",
      "commission_id": "cnc-talent-YYYY-MM-DD",
      "talent_raw": "Exact talent name as written on page",
      "talent_person_id": "",
      "beneficiary_raw": "Exact beneficiary name as written on page",
      "beneficiary_company_id": "",
      "amount": <integer in euros, e.g. 30000>,
      "aid_section": "aide_creation" | "aide_chaine",
      "aid_type": "standard" | "bourse_encouragement" | "aide_pilote" | "developpement_chaine",
      "result": "accepted" | "rejected"
    }
  ]
}

Rules:
- If the page contains MULTIPLE commission sessions (a consolidated page), return one object per session.
- Dates must be ISO 8601 (YYYY-MM-DD). Convert French dates: "12 novembre 2025" → "2025-11-12".
- Amounts must be integers in euros (strip spaces and "€": "30 000 €" → 30000).
- The "id" field uses the actual commission date, NOT the date in the URL slug (they sometimes differ).
- Grant "id" must be unique: use commission id + a short kebab-case slug of the project title.
- "aid_type" rules:
    - "bourse_encouragement" if amount is 2000 or the section says "bourse d'encouragement"
    - "aide_pilote" if amount is 5000 or the section says "aide au pilote"
    - "developpement_chaine" if in the "aide à la chaîne" section
    - "standard" otherwise
- "aid_section":
    - "aide_chaine" if the project is in a "Développement de la chaîne" / "Aide à la chaîne" section
    - "aide_creation" for everything else
- Leave all *_id fields (person_id, talent_person_id, etc.) as empty string "".
- For rejected projects: include them with result "rejected" and amount 0.
- Preserve accents and special characters exactly as on the page.`

// Scraper fetches CNC Talent commission pages and parses them via Claude API.
type Scraper struct {
	client  *anthropic.Client
	dataDir string
	http    *http.Client
}

// New creates a Scraper. apiKey defaults to ANTHROPIC_API_KEY env var if empty.
func New(apiKey, dataDir string) *Scraper {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	c := anthropic.NewClient(opts...)
	return &Scraper{
		client:  &c,
		dataDir: dataDir,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// ScrapeAll processes all known URLs, skipping ones already saved to disk.
func (s *Scraper) ScrapeAll(ctx context.Context) error {
	for i, u := range KnownURLs {
		log.Printf("[%d/%d] %s", i+1, len(KnownURLs), u.URL)

		commissions, err := s.Scrape(ctx, u)
		if err != nil {
			log.Printf("  ERROR: %v", err)
			continue
		}

		for _, c := range commissions {
			outPath := filepath.Join(s.dataDir, "raw", "commissions", c.ID+".json")
			if _, err := os.Stat(outPath); err == nil {
				log.Printf("  SKIP (already exists): %s", c.ID)
				continue
			}
			if err := store.SaveCommission(s.dataDir, c); err != nil {
				log.Printf("  ERROR saving %s: %v", c.ID, err)
				continue
			}
			log.Printf("  SAVED: %s (%d jury, %d grants)", c.ID, len(c.Jury), len(c.Grants))
		}

		// Polite delay between requests.
		time.Sleep(1 * time.Second)
	}
	return nil
}

// Scrape fetches a single URL and returns one or more Commission objects.
// Consolidated pages produce multiple commissions.
func (s *Scraper) Scrape(ctx context.Context, u CommissionURL) ([]*model.Commission, error) {
	html, err := s.fetchHTML(u.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	text := htmlToText(html)
	return s.parse(ctx, u.URL, text)
}

// parse sends the page text to Claude and returns structured Commission data.
func (s *Scraper) parse(ctx context.Context, sourceURL, text string) ([]*model.Commission, error) {
	userMsg := fmt.Sprintf("Source URL: %s\n\n---PAGE CONTENT---\n%s", sourceURL, text)

	stream := s.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5, // fast + cheap for extraction
		MaxTokens: 32768,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userMsg)),
		},
	})
	defer stream.Close()

	var msg anthropic.Message
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			return nil, fmt.Errorf("claude API accumulate: %w", err)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("claude API: %w", err)
	}

	raw := extractText(&msg)
	raw = strings.TrimSpace(raw)

	// Claude sometimes wraps in a markdown code fence despite instructions.
	raw = stripCodeFence(raw)

	var commissions []*model.Commission
	if err := json.Unmarshal([]byte(raw), &commissions); err != nil {
		return nil, fmt.Errorf("json parse failed: %w\nraw response:\n%s", err, raw)
	}
	return commissions, nil
}

// fetchHTML retrieves the raw HTML of a URL.
func (s *Scraper) fetchHTML(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CNC-research-bot/1.0)")
	req.Header.Set("Accept-Language", "fr-FR,fr;q=0.9")

	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// htmlToText does a lightweight HTML → plain text conversion:
// strips tags, decodes common HTML entities, collapses whitespace.
// This reduces token usage without losing semantic content.
func htmlToText(html string) string {
	// Remove script and style blocks entirely.
	html = removeBetween(html, "<script", "</script>")
	html = removeBetween(html, "<style", "</style>")
	html = removeBetween(html, "<nav", "</nav>")
	html = removeBetween(html, "<header", "</header>")
	html = removeBetween(html, "<footer", "</footer>")

	// Strip all remaining tags.
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
			b.WriteRune('\n') // newline at tag boundary helps preserve structure
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}

	text := b.String()

	// Decode common HTML entities.
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&nbsp;", " ",
		"&agrave;", "à",
		"&eacute;", "é",
		"&egrave;", "è",
		"&ecirc;", "ê",
		"&euml;", "ë",
		"&icirc;", "î",
		"&iuml;", "ï",
		"&ocirc;", "ô",
		"&ugrave;", "ù",
		"&ucirc;", "û",
		"&ccedil;", "ç",
		"&laquo;", "«",
		"&raquo;", "»",
		"&hellip;", "…",
	)
	text = replacer.Replace(text)

	// Collapse runs of blank lines.
	lines := strings.Split(text, "\n")
	var out []string
	prev := ""
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" && prev == "" {
			continue
		}
		out = append(out, l)
		prev = l
	}

	return strings.Join(out, "\n")
}

func removeBetween(s, open, close string) string {
	for {
		start := strings.Index(strings.ToLower(s), strings.ToLower(open))
		if start == -1 {
			break
		}
		end := strings.Index(strings.ToLower(s[start:]), strings.ToLower(close))
		if end == -1 {
			break
		}
		s = s[:start] + s[start+end+len(close):]
	}
	return s
}

func extractText(msg *anthropic.Message) string {
	for _, block := range msg.Content {
		if block.Type == "text" {
			return block.Text
		}
	}
	return ""
}

var codeFenceRe = regexp.MustCompile("(?s)^`{3,}[a-z]*\n(.*)\n`{3,}\\s*$")

func stripCodeFence(s string) string {
	if m := codeFenceRe.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	// Handle unclosed fence (truncated response): strip opening line only.
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			return strings.TrimSpace(s[idx+1:])
		}
	}
	return s
}
