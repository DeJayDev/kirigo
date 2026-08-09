package commute

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	routingpb "cloud.google.com/go/maps/routing/apiv2/routingpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/DeJayDev/kirigo/internal/googlemaps"
)

const DefaultMode = "driving"

var modeEnum = map[string]routingpb.RouteTravelMode{
	"driving":   routingpb.RouteTravelMode_DRIVE,
	"transit":   routingpb.RouteTravelMode_TRANSIT,
	"walking":   routingpb.RouteTravelMode_WALK,
	"bicycling": routingpb.RouteTravelMode_BICYCLE,
}

type routesClient interface {
	ComputeRoutes(ctx context.Context, req *routingpb.ComputeRoutesRequest, fieldMask string) (*routingpb.ComputeRoutesResponse, error)
}

type Config struct {
	Origin       string
	Destination  string
	Mode         string
	Alternatives bool
	Waypoints    []string
	Departure    string
	Arrival      string
	Tolls        bool
	Segments     bool
	APIKey       string
}

type Result struct {
	Status        string     `json:"status"`
	Mode          string     `json:"mode"`
	Origin        Place      `json:"origin"`
	Destination   Place      `json:"destination"`
	Routes        []Route    `json:"routes"`
	TrafficStatus string     `json:"traffic_status"`
	DepartAt      *time.Time `json:"depart_at,omitempty"`
	ArriveAt      *time.Time `json:"arrive_at,omitempty"`
	Timestamp     time.Time  `json:"timestamp"`
}

type Place struct {
	Address string  `json:"address"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
}

type Route struct {
	Duration       TextSeconds  `json:"duration"`
	StaticDuration TextSeconds  `json:"staticDuration"`
	Distance       TextDistance `json:"distance"`
	Description    string       `json:"description,omitempty"`
	Legs           []Leg        `json:"legs,omitempty"`
	Toll           *Toll        `json:"toll,omitempty"`
	Segments       []Segment    `json:"segments,omitempty"`
	Polyline       string       `json:"polyline,omitempty"`
}

type Leg struct {
	Duration       TextSeconds  `json:"duration"`
	StaticDuration TextSeconds  `json:"staticDuration"`
	Distance       TextDistance `json:"distance"`
	Start          Place        `json:"start"`
	End            Place        `json:"end"`
	Segments       []Segment    `json:"segments,omitempty"`
}

type Toll struct {
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
	Text     string  `json:"text"`
}

type Segment struct {
	Speed      string `json:"speed"`
	StartIndex int    `json:"start_index"`
	EndIndex   int    `json:"end_index"`
}

type TextSeconds struct {
	Text    string `json:"text"`
	Seconds int    `json:"seconds"`
}

type TextDistance struct {
	Text   string `json:"text"`
	Meters int    `json:"meters"`
}

func (c Config) Validate() error {
	_, err := c.Normalized()
	return err
}

func (c Config) Normalized() (Config, error) {
	c.Origin = strings.TrimSpace(c.Origin)
	c.Destination = strings.TrimSpace(c.Destination)
	c.Mode = strings.TrimSpace(c.Mode)
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.Arrival = strings.TrimSpace(c.Arrival)
	c.Departure = strings.TrimSpace(c.Departure)

	stops := make([]string, 0, len(c.Waypoints))
	for _, w := range c.Waypoints {
		if w = strings.TrimSpace(w); w != "" {
			stops = append(stops, w)
		}
	}
	c.Waypoints = stops

	if c.Origin == "" {
		return Config{}, errors.New("origin is required")
	}
	if c.Destination == "" {
		return Config{}, errors.New("destination is required")
	}
	if c.APIKey == "" {
		return Config{}, errors.New("Google Maps API key is required via GOOGLE_MAPS_API_KEY")
	}
	if c.Mode == "" {
		c.Mode = DefaultMode
	}
	if _, ok := modeEnum[c.Mode]; !ok {
		return Config{}, fmt.Errorf("mode %q is not supported (use driving, transit, walking, or bicycling)", c.Mode)
	}
	if c.Arrival != "" && c.Departure != "" {
		return Config{}, errors.New("specify -arrival or -departure, not both")
	}
	return c, nil
}

func Lookup(ctx context.Context, client routesClient, cfg Config, now func() time.Time) (Result, error) {
	cfg, err := cfg.Normalized()
	if err != nil {
		return Result{}, err
	}
	if now == nil {
		now = time.Now
	}
	nowT := now()

	departure, err := parseWhen(cfg.Departure, nowT)
	if err != nil {
		return Result{}, err
	}
	arrival, err := parseWhen(cfg.Arrival, nowT)
	if err != nil {
		return Result{}, err
	}

	mode := modeEnum[cfg.Mode]
	driving := mode == routingpb.RouteTravelMode_DRIVE
	mask := googlemaps.RoutesFieldMask(cfg.Tolls && driving, cfg.Segments && driving)

	var response *routingpb.ComputeRoutesResponse
	var departAt time.Time
	haveDepart := false

	switch {
	case !arrival.IsZero() && mode == routingpb.RouteTravelMode_TRANSIT:
		req := buildRequest(cfg, mode)
		req.ArrivalTime = timestamppb.New(arrival)
		response, err = client.ComputeRoutes(ctx, req, mask)
	case !arrival.IsZero():
		departAt, err = solveDeparture(ctx, client, cfg, mode, arrival, nowT)
		if err != nil {
			return Result{}, err
		}
		haveDepart = true
		req := buildRequest(cfg, mode)
		if driving && departAt.After(nowT) { // omit when clamped to "now" (already late)
			req.DepartureTime = timestamppb.New(departAt)
		}
		response, err = client.ComputeRoutes(ctx, req, mask)
	default:
		req := buildRequest(cfg, mode)
		if !departure.IsZero() {
			req.DepartureTime = timestamppb.New(departure)
			departAt = departure
			haveDepart = true
		}
		response, err = client.ComputeRoutes(ctx, req, mask)
	}
	if err != nil {
		return Result{}, err
	}
	if response == nil || len(response.GetRoutes()) == 0 {
		return Result{}, errors.New("Routes API returned no routes (check that origin, destination, and waypoints resolve to routable places)")
	}

	points := append([]string{cfg.Origin}, cfg.Waypoints...)
	points = append(points, cfg.Destination)

	routes := make([]Route, 0, len(response.GetRoutes()))
	var origin, destination Place
	for i, r := range response.GetRoutes() {
		converted, o, d, err := convertRoute(r, cfg, points)
		if err != nil {
			return Result{}, err
		}
		if i == 0 {
			origin, destination = o, d
		}
		routes = append(routes, converted)
	}

	result := Result{
		Status:        "ok",
		Mode:          cfg.Mode,
		Origin:        origin,
		Destination:   destination,
		Routes:        routes,
		TrafficStatus: classifyTraffic(routes[0].StaticDuration.Seconds, routes[0].Duration.Seconds),
		Timestamp:     nowT,
	}

	primary := time.Duration(routes[0].Duration.Seconds) * time.Second
	switch {
	case mode == routingpb.RouteTravelMode_TRANSIT && !arrival.IsZero():
		dep := arrival.Add(-primary)
		result.DepartAt, result.ArriveAt = &dep, &arrival
	case haveDepart:
		arr := departAt.Add(primary)
		result.DepartAt, result.ArriveAt = &departAt, &arr
	}
	return result, nil
}

func buildRequest(cfg Config, mode routingpb.RouteTravelMode) *routingpb.ComputeRoutesRequest {
	req := &routingpb.ComputeRoutesRequest{
		Origin:                   address(cfg.Origin),
		Destination:              address(cfg.Destination),
		TravelMode:               mode,
		ComputeAlternativeRoutes: cfg.Alternatives,
	}
	for _, w := range cfg.Waypoints {
		req.Intermediates = append(req.Intermediates, address(w))
	}
	if mode == routingpb.RouteTravelMode_DRIVE {
		req.RoutingPreference = routingpb.RoutingPreference_TRAFFIC_AWARE
		if cfg.Tolls {
			req.ExtraComputations = append(req.ExtraComputations, routingpb.ComputeRoutesRequest_TOLLS)
		}
		if cfg.Segments {
			req.ExtraComputations = append(req.ExtraComputations, routingpb.ComputeRoutesRequest_TRAFFIC_ON_POLYLINE)
		}
	}
	return req
}

func address(a string) *routingpb.Waypoint {
	return &routingpb.Waypoint{LocationType: &routingpb.Waypoint_Address{Address: a}}
}

// solveDeparture finds when to leave to arrive by arrival. Walking/bicycling
// durations don't vary with departure time, so one probe is exact; driving is
// traffic-aware, so it iterates departureTime until leave+duration converges.
func solveDeparture(ctx context.Context, client routesClient, cfg Config, mode routingpb.RouteTravelMode, arrival, now time.Time) (time.Time, error) {
	lite := cfg
	lite.Alternatives, lite.Tolls, lite.Segments = false, false, false
	mask := googlemaps.RoutesFieldMask(false, false)
	driving := mode == routingpb.RouteTravelMode_DRIVE

	// No departureTime on the probe: it means "now" server-side. A now timestamp
	// set here would already be in the past by the time the API validates it.
	probe := buildRequest(lite, mode)
	r, err := client.ComputeRoutes(ctx, probe, mask)
	if err != nil {
		return time.Time{}, err
	}
	if len(r.GetRoutes()) == 0 {
		return time.Time{}, errors.New("Routes API returned no routes")
	}
	depart := arrival.Add(-time.Duration(routeSeconds(r.GetRoutes()[0])) * time.Second)
	if !driving {
		if depart.Before(now) {
			depart = now
		}
		return depart, nil
	}
	for range 4 {
		if !depart.After(now) {
			return now, nil // can't leave in the past; already cutting it close
		}
		req := buildRequest(lite, mode)
		req.DepartureTime = timestamppb.New(depart)
		r, err := client.ComputeRoutes(ctx, req, mask)
		if err != nil {
			return time.Time{}, err
		}
		if len(r.GetRoutes()) == 0 {
			return time.Time{}, errors.New("Routes API returned no routes")
		}
		predicted := depart.Add(time.Duration(routeSeconds(r.GetRoutes()[0])) * time.Second)
		diff := arrival.Sub(predicted)
		if diff < 30*time.Second && diff > -30*time.Second {
			return depart, nil
		}
		depart = depart.Add(diff)
	}
	return depart, nil
}

func routeSeconds(r *routingpb.Route) int {
	return int(r.GetDuration().GetSeconds())
}

func convertRoute(r *routingpb.Route, cfg Config, points []string) (Route, Place, Place, error) {
	if len(r.GetLegs()) == 0 {
		return Route{}, Place{}, Place{}, errors.New("Routes API route has no legs")
	}
	traffic := int(r.GetDuration().GetSeconds())
	static := int(r.GetStaticDuration().GetSeconds())

	out := Route{
		Duration:       TextSeconds{Text: googlemaps.FormatDuration(traffic), Seconds: traffic},
		StaticDuration: TextSeconds{Text: googlemaps.FormatDuration(static), Seconds: static},
		Distance:       TextDistance{Text: formatDistance(int(r.GetDistanceMeters())), Meters: int(r.GetDistanceMeters())},
		Description:    r.GetDescription(),
	}
	if cfg.Segments || len(r.GetLegs()) > 1 {
		for i, leg := range r.GetLegs() {
			out.Legs = append(out.Legs, toLeg(leg, labelAt(points, i), labelAt(points, i+1)))
		}
	}
	if cfg.Tolls {
		out.Toll = toToll(r.GetTravelAdvisory().GetTollInfo())
	}
	if cfg.Segments {
		out.Segments = toSegments(r.GetTravelAdvisory().GetSpeedReadingIntervals())
		out.Polyline = r.GetPolyline().GetEncodedPolyline()
	}

	first := r.GetLegs()[0]
	last := r.GetLegs()[len(r.GetLegs())-1]
	origin := Place{Address: cfg.Origin, Lat: roundCoordinate(first.GetStartLocation().GetLatLng().GetLatitude()), Lng: roundCoordinate(first.GetStartLocation().GetLatLng().GetLongitude())}
	destination := Place{Address: cfg.Destination, Lat: roundCoordinate(last.GetEndLocation().GetLatLng().GetLatitude()), Lng: roundCoordinate(last.GetEndLocation().GetLatLng().GetLongitude())}
	return out, origin, destination, nil
}

func toLeg(leg *routingpb.RouteLeg, startAddr, endAddr string) Leg {
	d := int(leg.GetDuration().GetSeconds())
	sd := int(leg.GetStaticDuration().GetSeconds())
	return Leg{
		Duration:       TextSeconds{Text: googlemaps.FormatDuration(d), Seconds: d},
		StaticDuration: TextSeconds{Text: googlemaps.FormatDuration(sd), Seconds: sd},
		Distance:       TextDistance{Text: formatDistance(int(leg.GetDistanceMeters())), Meters: int(leg.GetDistanceMeters())},
		Start:          Place{Address: startAddr, Lat: roundCoordinate(leg.GetStartLocation().GetLatLng().GetLatitude()), Lng: roundCoordinate(leg.GetStartLocation().GetLatLng().GetLongitude())},
		End:            Place{Address: endAddr, Lat: roundCoordinate(leg.GetEndLocation().GetLatLng().GetLatitude()), Lng: roundCoordinate(leg.GetEndLocation().GetLatLng().GetLongitude())},
		Segments:       toSegments(leg.GetTravelAdvisory().GetSpeedReadingIntervals()),
	}
}

func toToll(ti *routingpb.TollInfo) *Toll {
	if ti == nil || len(ti.GetEstimatedPrice()) == 0 {
		return nil
	}
	m := ti.GetEstimatedPrice()[0]
	amount := float64(m.GetUnits()) + float64(m.GetNanos())/1e9
	return &Toll{Currency: m.GetCurrencyCode(), Amount: amount, Text: fmt.Sprintf("%.2f %s", amount, m.GetCurrencyCode())}
}

func toSegments(intervals []*routingpb.SpeedReadingInterval) []Segment {
	if len(intervals) == 0 {
		return nil
	}
	out := make([]Segment, 0, len(intervals))
	for _, iv := range intervals {
		out = append(out, Segment{
			Speed:      strings.ToLower(iv.GetSpeed().String()),
			StartIndex: int(iv.GetStartPolylinePointIndex()),
			EndIndex:   int(iv.GetEndPolylinePointIndex()),
		})
	}
	return out
}

func labelAt(points []string, i int) string {
	if i >= 0 && i < len(points) {
		return points[i]
	}
	return ""
}

// parseWhen accepts RFC3339 or a local clock time (HH:MM, 3:04pm, 5pm); a clock
// time already past today rolls to tomorrow.
func parseWhen(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	for _, layout := range []string{"15:04", "3:04pm", "3:04PM", "3pm", "3PM"} {
		if t, err := time.ParseInLocation(layout, value, now.Location()); err == nil {
			at := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
			if at.Before(now) {
				at = at.AddDate(0, 0, 1)
			}
			return at, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q (use HH:MM, 3:04pm, or RFC3339)", value)
}

func classifyTraffic(staticSeconds, trafficSeconds int) string {
	if staticSeconds <= 0 {
		return "unknown"
	}
	if trafficSeconds <= staticSeconds {
		return "light"
	}
	delayRatio := float64(trafficSeconds-staticSeconds) / float64(staticSeconds)
	switch {
	case delayRatio > 0.5:
		return "heavy"
	case delayRatio >= 0.2:
		return "moderate"
	default:
		return "light"
	}
}

func formatDistance(meters int) string {
	return fmt.Sprintf("%.1f mi", float64(meters)/1609.344)
}

func roundCoordinate(value float64) float64 {
	return math.Round(value*1_000_000) / 1_000_000
}
