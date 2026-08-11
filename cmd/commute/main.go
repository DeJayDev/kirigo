package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/alecthomas/kong"

	"github.com/DeJayDev/kirigo/internal/commute"
	"github.com/DeJayDev/kirigo/internal/configenv"
	"github.com/DeJayDev/kirigo/internal/googlemaps"
	"github.com/DeJayDev/kirigo/internal/output"
)

type CLI struct {
	Format       string   `help:"output format: json (default) or toon; overrides KIRIGO_FORMAT"`
	Origin       string   `help:"origin address, place, or lat,lng"`
	Destination  string   `help:"destination address, place, or lat,lng"`
	Mode         string   `default:"driving" help:"travel mode: driving, transit, walking, bicycling"`
	Alternatives bool     `help:"include alternative routes"`
	Waypoint     []string `help:"intermediate stop, ordered (repeatable)"`
	Arrival      string   `help:"arrive-by time (HH:MM, 3:04pm, or RFC3339); computes when to leave"`
	Departure    string   `help:"depart-at time (default now)"`
	Tolls        bool     `help:"include toll cost when the API returns it"`
	Segments     bool     `help:"include per-leg and traffic-speed segments (where it's slow)"`
}

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if err := configenv.LoadDefault(); err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}

	var cli CLI
	parser, err := kong.New(&cli, kong.Name("commute"),
		kong.Description("Real-time driving times from the Google Routes API."))
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}
	if _, err := parser.Parse(args); err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}

	outFmt, err := output.ResolveFormat(cli.Format)
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}

	cfg := commute.Config{
		Origin:       cli.Origin,
		Destination:  cli.Destination,
		Mode:         cli.Mode,
		Alternatives: cli.Alternatives,
		Waypoints:    cli.Waypoint,
		Arrival:      cli.Arrival,
		Departure:    cli.Departure,
		Tolls:        cli.Tolls,
		Segments:     cli.Segments,
		APIKey:       os.Getenv("GOOGLE_MAPS_API_KEY"),
	}
	cfg, err = cfg.Normalized()
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), outFmt)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := commute.Lookup(ctx, googlemaps.NewClient(cfg.APIKey), cfg, time.Now)
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), outFmt)
		return 1
	}

	if err := output.Write(os.Stdout, result, outFmt); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write output: %v\n", err)
		return 1
	}
	return 0
}
