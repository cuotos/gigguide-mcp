package main

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

// Gig represents a single live music event.
type Gig struct {
	Date      string `json:"date"`                 // YYYY-MM-DD
	Time      string `json:"time,omitempty"`       // HH:MM
	Venue     string `json:"venue"`
	VenueURL  string `json:"venue_url,omitempty"`
	Location  string `json:"location,omitempty"`
	Artist    string `json:"artist"`
	ArtistURL string `json:"artist_url,omitempty"`
}

// ── Cache ────────────────────────────────────────────────────────────────────

var (
	cacheMu  sync.Mutex
	cached   []Gig
	cacheAt  time.Time
	cacheTTL = time.Hour
)

func getGigs() ([]Gig, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if cached != nil && time.Since(cacheAt) < cacheTTL {
		return cached, nil
	}

	gigs, err := fetchAndParse()
	if err != nil {
		return nil, err
	}
	cached = gigs
	cacheAt = time.Now()
	return gigs, nil
}

// ── HTTP fetch ───────────────────────────────────────────────────────────────

func fetchAndParse() ([]Gig, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", "https://www.rock-regeneration.co.uk/gig-guide/", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; gigguide-mcp/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP error: %w", err)
	}
	defer resp.Body.Close()

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("HTML parse error: %w", err)
	}

	return parseGigs(doc), nil
}

// ── Parsing ──────────────────────────────────────────────────────────────────
//
// The page structure is:
//
//	<strong>Mon 16th Mar 2026</strong>
//	<table>
//	  <tr>
//	    <td width="35%"><a href="...">Venue Name</a></td>
//	    <td width="65%">18:00: Town, <a href="...">Artist Name</a>- Notes</td>
//	  </tr>
//	  ...
//	</table>
//	<strong>Tue 17th Mar 2026</strong>
//	<table>...</table>
//
// The content is not inside a labelled container, so we walk the full document
// watching for date-strong tags and their following tables.

func parseGigs(doc *html.Node) []Gig {
	var gigs []Gig
	var currentDate string

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		// Skip sidebar — it has its own <strong> tags we don't want.
		if n.Type == html.ElementNode && isSkippable(n) {
			return
		}

		if n.Type == html.ElementNode {
			switch n.Data {
			case "strong":
				if text := nodeText(n); text != "" {
					if d, ok := parseDate(text); ok {
						currentDate = d
					}
				}

			case "table":
				if currentDate != "" {
					gigs = append(gigs, parseTable(n, currentDate)...)
					return // don't recurse further into this table
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return gigs
}

// isSkippable returns true for elements whose contents should be ignored
// (sidebar, nav, header, footer).
func isSkippable(n *html.Node) bool {
	switch n.Data {
	case "nav", "header", "footer":
		return true
	}
	for _, a := range n.Attr {
		if a.Key == "class" {
			for cls := range strings.FieldsSeq(a.Val) {
				switch cls {
				case "sidebar", "sidebar-content", "sidebar-top",
					"page-title", "pagination":
					return true
				}
			}
		}
		if a.Key == "id" && (a.Val == "header" || a.Val == "footer") {
			return true
		}
	}
	return false
}

// parseTable extracts one Gig per <tr>.
func parseTable(table *html.Node, date string) []Gig {
	var gigs []Gig
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			if g := parseRow(n, date); g != nil {
				gigs = append(gigs, *g)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(table)
	return gigs
}

// parseRow parses one <tr> into a Gig.
//
// Expected columns:
//
//	td[0] (35%): <a href="venue-url">Venue Name</a>
//	td[1] (65%): HH:MM: Location, <a href="artist-url">Artist</a>- Optional notes
func parseRow(tr *html.Node, date string) *Gig {
	var tds []*html.Node
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "td" {
			tds = append(tds, c)
		}
	}
	if len(tds) < 2 {
		return nil
	}

	venueText, venueHref := firstLink(tds[0])
	if venueText == "" {
		venueText = strings.TrimSpace(nodeText(tds[0]))
	}
	if venueText == "" {
		return nil
	}

	g := &Gig{Date: date, Venue: venueText, VenueURL: venueHref}

	// Parse the event cell: "HH:MM: Location, Artist- Notes"
	eventText := strings.TrimSpace(nodeText(tds[1]))

	loc := timeRe.FindStringIndex(eventText)
	if loc == nil {
		g.Artist = eventText
		return g
	}

	g.Time = eventText[loc[0]:loc[1]]

	after := strings.TrimSpace(eventText[loc[1]:])
	after = strings.TrimPrefix(after, ":")
	after = strings.TrimSpace(after)

	// Split on first comma → Location, rest
	if loc, rest, ok := strings.Cut(after, ","); ok {
		g.Location = strings.TrimSpace(loc)
		g.Artist = strings.TrimSpace(rest)
	} else {
		g.Artist = after
	}

	// Artist URL: first link in the event cell.
	if _, href := firstLink(tds[1]); href != "" {
		g.ArtistURL = href
	}

	return g
}

// ── HTML helpers ─────────────────────────────────────────────────────────────

var timeRe = regexp.MustCompile(`\b(\d{1,2}:\d{2})\b`)

func nodeText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(sb.String())
}

func firstLink(n *html.Node) (text, href string) {
	var found bool
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found {
			return
		}
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key == "href" {
					href = a.Val
				}
			}
			text = nodeText(n)
			if href != "" {
				found = true
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return
}

// ── Date parsing ─────────────────────────────────────────────────────────────

var monthNames = map[string]time.Month{
	"jan": time.January, "january": time.January,
	"feb": time.February, "february": time.February,
	"mar": time.March, "march": time.March,
	"apr": time.April, "april": time.April,
	"may": time.May,
	"jun": time.June, "june": time.June,
	"jul": time.July, "july": time.July,
	"aug": time.August, "august": time.August,
	"sep": time.September, "september": time.September,
	"oct": time.October, "october": time.October,
	"nov": time.November, "november": time.November,
	"dec": time.December, "december": time.December,
}

// dateRe matches "Mon 16th Mar 2026" style.
var dateRe = regexp.MustCompile(`(?i)(\d{1,2})(?:st|nd|rd|th)?\s+(\w+)\s+(\d{4})`)

// dateRe2 matches "Monday, March 16, 2026" style.
var dateRe2 = regexp.MustCompile(`(?i)([A-Za-z]+)\s+(\d{1,2})(?:st|nd|rd|th)?,?\s+(\d{4})`)

func parseDate(text string) (string, bool) {
	if m := dateRe.FindStringSubmatch(text); m != nil {
		day := atoi(m[1])
		month := monthNames[strings.ToLower(m[2])]
		year := atoi(m[3])
		if month != 0 && day > 0 && year > 2000 {
			return time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), true
		}
	}
	if m := dateRe2.FindStringSubmatch(text); m != nil {
		month := monthNames[strings.ToLower(m[1])]
		day := atoi(m[2])
		year := atoi(m[3])
		if month != 0 && day > 0 && year > 2000 {
			return time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), true
		}
	}
	return "", false
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if unicode.IsDigit(c) {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

// ── Filtering ────────────────────────────────────────────────────────────────

func filterGigs(gigs []Gig, p searchParams) []Gig {
	var fromDate, toDate time.Time
	if p.FromDate != "" {
		fromDate, _ = time.Parse("2006-01-02", p.FromDate)
	}
	if p.ToDate != "" {
		toDate, _ = time.Parse("2006-01-02", p.ToDate)
	}

	var locationTokens []string
	if p.Location != "" {
		locationTokens = nearbyTowns(p.Location)
	}

	var out []Gig
	for _, g := range gigs {
		if !fromDate.IsZero() || !toDate.IsZero() {
			gd, err := time.Parse("2006-01-02", g.Date)
			if err != nil {
				continue
			}
			if !fromDate.IsZero() && gd.Before(fromDate) {
				continue
			}
			if !toDate.IsZero() && gd.After(toDate) {
				continue
			}
		}

		if len(locationTokens) > 0 {
			if !matchesAny(g.Location+" "+g.Venue, locationTokens) {
				continue
			}
		}

		if p.Artist != "" && !containsCI(g.Artist, p.Artist) {
			continue
		}
		if p.Venue != "" && !containsCI(g.Venue, p.Venue) {
			continue
		}

		out = append(out, g)
	}
	return out
}

func matchesAny(haystack string, needles []string) bool {
	h := strings.ToLower(haystack)
	for _, n := range needles {
		if strings.Contains(h, strings.ToLower(n)) {
			return true
		}
	}
	return false
}

func containsCI(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// ── Region map ───────────────────────────────────────────────────────────────

func nearbyTowns(location string) []string {
	loc := strings.ToLower(strings.TrimSpace(location))
	for _, region := range regions {
		for _, town := range region {
			if strings.Contains(strings.ToLower(town), loc) || strings.Contains(loc, strings.ToLower(town)) {
				return region
			}
		}
	}
	return []string{location}
}

var regions = [][]string{
	// Southampton / central Hampshire
	{"Southampton", "Eastleigh", "Winchester", "Hedge End", "Totton", "Chandler's Ford",
		"Fair Oak", "Bishopstoke", "Bursledon", "Netley", "West End", "Bitterne", "Shirley"},
	// Bournemouth / Poole / east Dorset
	{"Bournemouth", "Poole", "Christchurch", "Boscombe", "Parkstone", "Ferndown",
		"Wimborne", "Verwood", "Swanage"},
	// Weymouth / west Dorset
	{"Weymouth", "Dorchester", "Portland", "Bridport", "Sherborne"},
	// Portsmouth / Fareham
	{"Portsmouth", "Southsea", "Fareham", "Gosport", "Havant", "Waterlooville",
		"Cosham", "Emsworth", "Petersfield"},
	// Salisbury / Wiltshire south
	{"Salisbury", "Amesbury", "Andover", "Tidworth", "Fordingbridge"},
	// Isle of Wight
	{"Newport", "Ryde", "Cowes", "Ventnor", "Sandown", "Shanklin", "Freshwater"},
	// New Forest / west Hampshire coast
	{"Ringwood", "Lymington", "New Milton", "Brockenhurst", "Lyndhurst", "Hythe",
		"Marchwood", "Burley"},
	// North Hampshire / Basingstoke
	{"Basingstoke", "Alton", "Alresford", "Hook", "Fleet", "Farnborough", "Farnham",
		"Camberley", "Tadley", "Whitchurch"},
	// Bath / Bristol fringe
	{"Bath", "Bristol", "Trowbridge", "Frome", "Warminster"},
}
