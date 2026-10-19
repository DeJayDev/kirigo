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
	Format string `help:"output format: json (default) or toon; overrides KIRIGO_FORMAT"`

	Route  RouteCmd  `cmd:"" default:"withargs" help:"compute a route (default)"`
	Search SearchCmd `cmd:"" help:"search places by text"`
}

type RouteCmd struct {
	Origin       string   `help:"origin address, lat,lng, named place, or place_id:<id>"`
	Destination  string   `help:"destination address, lat,lng, named place, or place_id:<id>"`
	Mode         string   `default:"driving" help:"travel mode: driving, transit, walking, bicycling"`
	Alternatives bool     `help:"include alternative routes"`
	Waypoint     []string `sep:"none" help:"intermediate stop, ordered (repeatable)"`
	Arrival      string   `help:"arrive-by time (HH:MM, 3:04pm, or RFC3339); computes when to leave"`
	Departure    string   `help:"depart-at time (default now)"`
	Tolls        bool     `help:"include toll cost when the API returns it"`
	Segments     bool     `help:"include per-leg times, traffic-speed segments, and the street sequence"`
}

type SearchCmd struct {
	Query string `arg:"" help:"what to find, e.g. \"coffee\""`
	Near  string `help:"search near this address or named place"`
}

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if err := configenv.LoadDefault(); err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}

	var cli CLI
	parser, err := kong.New(&cli, kong.Name("commute"),
		kong.Description("Real-time driving times and place search from the Google Routes and Places APIs."))
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}

	outFmt, err := output.ResolveFormat(cli.Format)
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}

	places, err := configenv.LoadPlaces()
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), outFmt)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result any
	if kctx.Command() == "search <query>" {
		apiKey := os.Getenv("GOOGLE_MAPS_API_KEY")
		if apiKey == "" {
			_ = output.WriteError(os.Stderr, "missing Google Maps API key; set GOOGLE_MAPS_API_KEY", outFmt)
			return 2
		}
		result, err = commute.Search(ctx, googlemaps.NewClient(apiKey), cli.Search.Query, cli.Search.Near, places)
	} else {
		cfg := commute.Config{
			Origin:       cli.Route.Origin,
			Destination:  cli.Route.Destination,
			Mode:         cli.Route.Mode,
			Alternatives: cli.Route.Alternatives,
			Waypoints:    cli.Route.Waypoint,
			Arrival:      cli.Route.Arrival,
			Departure:    cli.Route.Departure,
			Tolls:        cli.Route.Tolls,
			Segments:     cli.Route.Segments,
			APIKey:       os.Getenv("GOOGLE_MAPS_API_KEY"),
			Places:       places,
		}
		cfg, err = cfg.Normalized()
		if err != nil {
			_ = output.WriteError(os.Stderr, err.Error(), outFmt)
			return 2
		}
		result, err = commute.Lookup(ctx, googlemaps.NewClient(cfg.APIKey), cfg, time.Now)
	}
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
