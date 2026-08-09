package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/DeJayDev/kirigo/internal/commute"
	"github.com/DeJayDev/kirigo/internal/configenv"
	"github.com/DeJayDev/kirigo/internal/googlemaps"
	"github.com/DeJayDev/kirigo/internal/output"
)

func main() {
	os.Exit(run())
}

func run() int {
	if err := configenv.LoadDefault(); err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}

	var cfg commute.Config
	var waypoints stringSlice
	flag.StringVar(&cfg.Origin, "origin", "", "origin address, place, or lat,lng")
	flag.StringVar(&cfg.Destination, "destination", "", "destination address, place, or lat,lng")
	flag.StringVar(&cfg.Mode, "mode", commute.DefaultMode, "travel mode: driving, transit, walking, bicycling")
	flag.BoolVar(&cfg.Alternatives, "alternatives", false, "include alternative routes")
	flag.StringVar(&cfg.Arrival, "arrival", "", "arrive-by time (HH:MM, 3:04pm, or RFC3339); computes when to leave")
	flag.StringVar(&cfg.Departure, "departure", "", "depart-at time (default now)")
	flag.Var(&waypoints, "waypoint", "intermediate stop, ordered (repeatable)")
	flag.BoolVar(&cfg.Tolls, "tolls", false, "include toll cost when the API returns it")
	flag.BoolVar(&cfg.Segments, "segments", false, "include per-leg and traffic-speed segments (where it's slow)")
	format := output.RegisterFlag(flag.CommandLine)
	cfg.APIKey = os.Getenv("GOOGLE_MAPS_API_KEY")
	flag.Parse()
	cfg.Waypoints = waypoints

	outFmt, err := output.ResolveFormat(*format)
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), "json")
		return 2
	}

	cfg, err = cfg.Normalized()
	if err != nil {
		_ = output.WriteError(os.Stderr, err.Error(), outFmt)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := googlemaps.NewClient(cfg.APIKey)
	result, err := commute.Lookup(ctx, client, cfg, time.Now)
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

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}
