package main

import (
	"context"
	"flag"
	"log"
	"os"

	"cnc/internal/scraper"
)

func main() {
	dataDir := flag.String("data", "data", "path to data directory")
	apiKey := flag.String("key", "", "Anthropic API key (defaults to ANTHROPIC_API_KEY env var)")
	url := flag.String("url", "", "scrape a single URL instead of all known URLs")
	flag.Parse()

	key := *apiKey
	if key == "" {
		key = os.Getenv("ANTHROPIC_API_KEY")
	}
	if key == "" {
		log.Fatal("ANTHROPIC_API_KEY not set")
	}

	s := scraper.New(key, *dataDir)
	ctx := context.Background()

	if *url != "" {
		commissions, err := s.Scrape(ctx, scraper.CommissionURL{URL: *url})
		if err != nil {
			log.Fatalf("scrape %s: %v", *url, err)
		}
		for _, c := range commissions {
			log.Printf("Parsed: %s (%d jury, %d grants)", c.ID, len(c.Jury), len(c.Grants))
		}
		return
	}

	if err := s.ScrapeAll(ctx); err != nil {
		log.Fatalf("scrape all: %v", err)
	}
}
