package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime/debug"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value[:min(7, len(s.Value))]
		}
	}
	return "dev"
}

func main() {
	log.SetOutput(os.Stderr)

	s := server.NewMCPServer("gigguide-mcp", buildVersion(),
		server.WithToolCapabilities(false),
	)

	searchGigs := mcp.NewTool("search_gigs",
		mcp.WithDescription("Search for upcoming gigs and live music events from the Rock Regeneration gig guide, covering venues across southern England (Hampshire, Dorset, Wiltshire, Isle of Wight, etc). Returns structured event data. All filters are optional — omit to return everything."),
		mcp.WithString("location",
			mcp.Description("Filter by town or city. Automatically expands to include nearby towns in the same area (e.g. 'Southampton' also returns Eastleigh, Winchester, Hedge End, etc.)."),
		),
		mcp.WithString("artist",
			mcp.Description("Filter by artist or band name — partial, case-insensitive match."),
		),
		mcp.WithString("venue",
			mcp.Description("Filter by venue name — partial, case-insensitive match."),
		),
		mcp.WithString("from_date",
			mcp.Description("Only return gigs on or after this date. Format: YYYY-MM-DD."),
		),
		mcp.WithString("to_date",
			mcp.Description("Only return gigs on or before this date. Format: YYYY-MM-DD."),
		),
	)

	s.AddTool(searchGigs, handleSearchGigs)

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

type searchParams struct {
	Location string `json:"location"`
	Artist   string `json:"artist"`
	Venue    string `json:"venue"`
	FromDate string `json:"from_date"`
	ToDate   string `json:"to_date"`
}

func handleSearchGigs(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	params := searchParams{
		Location: stringArg(args, "location"),
		Artist:   stringArg(args, "artist"),
		Venue:    stringArg(args, "venue"),
		FromDate: stringArg(args, "from_date"),
		ToDate:   stringArg(args, "to_date"),
	}

	gigs, err := getGigs()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch gigs: %w", err)
	}

	results := filterGigs(gigs, params)

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal results: %w", err)
	}

	text := fmt.Sprintf("Found %d gig(s).\n\n%s", len(results), string(data))
	return mcp.NewToolResultText(text), nil
}

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}
