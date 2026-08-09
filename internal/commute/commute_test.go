package commute

import (
	"context"
	"testing"
	"time"

	routingpb "cloud.google.com/go/maps/routing/apiv2/routingpb"
	"google.golang.org/genproto/googleapis/type/latlng"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/protobuf/types/known/durationpb"
)

type fakeRoutesClient struct {
	requests []*routingpb.ComputeRoutesRequest
	response *routingpb.ComputeRoutesResponse
	err      error
}

func (f *fakeRoutesClient) ComputeRoutes(_ context.Context, req *routingpb.ComputeRoutesRequest, _ string) (*routingpb.ComputeRoutesResponse, error) {
	f.requests = append(f.requests, req)
	return f.response, f.err
}

func loc(lat, lng float64) *routingpb.Location {
	return &routingpb.Location{LatLng: &latlng.LatLng{Latitude: lat, Longitude: lng}}
}

func secs(n int) *durationpb.Duration { return durationpb.New(time.Duration(n) * time.Second) }

func resp(routes ...*routingpb.Route) *routingpb.ComputeRoutesResponse {
	return &routingpb.ComputeRoutesResponse{Routes: routes}
}

func TestLookupReturnsCommuteResult(t *testing.T) {
	client := &fakeRoutesClient{response: resp(&routingpb.Route{
		DistanceMeters: 6759,
		Duration:       secs(1440),
		StaticDuration: secs(900),
		Description:    "US-101 N",
		Legs: []*routingpb.RouteLeg{{
			StartLocation: loc(37.7749123, -122.4194567),
			EndLocation:   loc(37.8044567, -122.4108123),
		}},
	})}

	now := time.Date(2026, 5, 20, 11, 41, 0, 0, time.FixedZone("PDT", -7*60*60))
	result, err := Lookup(context.Background(), client, Config{
		Origin:      "123 Main St, SF",
		Destination: "1 Ferry Building, SF",
		Mode:        DefaultMode,
		APIKey:      "test-key",
	}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}

	if client.requests[0].GetTravelMode() != routingpb.RouteTravelMode_DRIVE {
		t.Fatalf("travelMode = %v, want DRIVE", client.requests[0].GetTravelMode())
	}
	if result.Status != "ok" || result.Mode != "driving" {
		t.Fatalf("status/mode = %q/%q", result.Status, result.Mode)
	}
	if result.Origin.Address != "123 Main St, SF" || result.Origin.Lat != 37.774912 || result.Origin.Lng != -122.419457 {
		t.Fatalf("origin = %#v", result.Origin)
	}
	if result.Destination.Lat != 37.804457 || result.Destination.Lng != -122.410812 {
		t.Fatalf("destination = %#v", result.Destination)
	}
	route := result.Routes[0]
	if route.Duration.Seconds != 1440 || route.StaticDuration.Seconds != 900 {
		t.Fatalf("durations = %#v %#v", route.Duration, route.StaticDuration)
	}
	if route.Distance.Meters != 6759 || route.Distance.Text != "4.2 mi" {
		t.Fatalf("distance = %#v", route.Distance)
	}
	if route.Description != "US-101 N" {
		t.Fatalf("description = %q", route.Description)
	}
	if result.TrafficStatus != "heavy" {
		t.Fatalf("traffic status = %q, want heavy", result.TrafficStatus)
	}
	if !result.Timestamp.Equal(now) {
		t.Fatalf("timestamp = %v, want %v", result.Timestamp, now)
	}
	if result.DepartAt != nil || result.ArriveAt != nil {
		t.Fatal("plain query should not set depart_at/arrive_at")
	}
}

func TestValidateRequiresAPIKey(t *testing.T) {
	if err := (Config{Origin: "a", Destination: "b", Mode: DefaultMode}).Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestArriveByDrivingSolvesDeparture(t *testing.T) {
	// constant 20-minute drive regardless of departureTime -> converges immediately.
	client := &fakeRoutesClient{response: resp(&routingpb.Route{
		DistanceMeters: 10000, Duration: secs(1200), StaticDuration: secs(1000),
		Legs: []*routingpb.RouteLeg{{StartLocation: loc(37.1, -122.1), EndLocation: loc(37.2, -122.2)}},
	})}
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	result, err := Lookup(context.Background(), client, Config{
		Origin: "a", Destination: "b", Mode: "driving",
		Arrival: "2026-08-05T13:00:00Z", APIKey: "k",
	}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if result.DepartAt == nil || result.ArriveAt == nil {
		t.Fatal("expected depart_at and arrive_at")
	}
	wantDepart := time.Date(2026, 8, 5, 12, 40, 0, 0, time.UTC)
	if d := result.DepartAt.Sub(wantDepart); d > 30*time.Second || d < -30*time.Second {
		t.Errorf("depart_at = %v, want ~12:40", result.DepartAt.UTC())
	}
	if last := client.requests[len(client.requests)-1]; last.GetDepartureTime() == nil {
		t.Fatal("final request should carry a departureTime")
	}
}

func TestWaypointLegsLabeled(t *testing.T) {
	client := &fakeRoutesClient{response: resp(&routingpb.Route{
		DistanceMeters: 20000, Duration: secs(1800), StaticDuration: secs(1680),
		Legs: []*routingpb.RouteLeg{
			{Duration: secs(600), StaticDuration: secs(540), DistanceMeters: 8000, StartLocation: loc(1, 1), EndLocation: loc(2, 2)},
			{Duration: secs(1200), StaticDuration: secs(1140), DistanceMeters: 12000, StartLocation: loc(2, 2), EndLocation: loc(3, 3)},
		},
	})}
	now := time.Now()
	result, err := Lookup(context.Background(), client, Config{
		Origin: "A", Destination: "C", Waypoints: []string{"B"}, Mode: "driving", APIKey: "k",
	}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	legs := result.Routes[0].Legs
	if len(legs) != 2 {
		t.Fatalf("legs = %d, want 2", len(legs))
	}
	if legs[0].Start.Address != "A" || legs[0].End.Address != "B" {
		t.Errorf("leg0 = %s -> %s", legs[0].Start.Address, legs[0].End.Address)
	}
	if legs[1].Start.Address != "B" || legs[1].End.Address != "C" {
		t.Errorf("leg1 = %s -> %s", legs[1].Start.Address, legs[1].End.Address)
	}
	// the intermediate stop must reach the request
	if got := client.requests[0].GetIntermediates(); len(got) != 1 || got[0].GetAddress() != "B" {
		t.Errorf("intermediates = %#v", got)
	}
}

func TestTollAndSegmentMapping(t *testing.T) {
	toll := toToll(&routingpb.TollInfo{EstimatedPrice: []*money.Money{{CurrencyCode: "USD", Units: 7, Nanos: 500_000_000}}})
	if toll == nil || toll.Amount != 7.5 || toll.Currency != "USD" {
		t.Fatalf("toll = %#v", toll)
	}
	segs := toSegments([]*routingpb.SpeedReadingInterval{{
		StartPolylinePointIndex: proto32(2), EndPolylinePointIndex: proto32(5),
		SpeedType: &routingpb.SpeedReadingInterval_Speed_{Speed: routingpb.SpeedReadingInterval_TRAFFIC_JAM},
	}})
	if len(segs) != 1 || segs[0].Speed != "traffic_jam" || segs[0].StartIndex != 2 || segs[0].EndIndex != 5 {
		t.Fatalf("segments = %#v", segs)
	}
}

func proto32(v int32) *int32 { return &v }
