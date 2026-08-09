package googlemaps

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	routingpb "cloud.google.com/go/maps/routing/apiv2/routingpb"
	"google.golang.org/protobuf/encoding/protojson"
)

func addr(a string) *routingpb.Waypoint {
	return &routingpb.Waypoint{LocationType: &routingpb.Waypoint_Address{Address: a}}
}

func TestComputeRoutesCallsRoutesAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("X-Goog-Api-Key"); got != "test-key" {
			t.Fatalf("api key = %q", got)
		}
		if got := r.Header.Get("X-Goog-FieldMask"); !strings.Contains(got, "routes.duration") {
			t.Fatalf("field mask = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req routingpb.ComputeRoutesRequest
		if err := protojson.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got := req.GetOrigin().GetAddress(); got != "123 Main St, SF" {
			t.Fatalf("origin = %q", got)
		}
		if req.GetTravelMode() != routingpb.RouteTravelMode_DRIVE {
			t.Fatalf("travelMode = %v", req.GetTravelMode())
		}
		if req.GetRoutingPreference() != routingpb.RoutingPreference_TRAFFIC_AWARE {
			t.Fatalf("routingPreference = %v", req.GetRoutingPreference())
		}
		if !req.GetComputeAlternativeRoutes() {
			t.Fatal("computeAlternativeRoutes = false, want true")
		}
		_, _ = w.Write([]byte(`{"routes":[{"description":"US-101 N","distanceMeters":6759,"duration":"1440s","staticDuration":"900s","legs":[{"startLocation":{"latLng":{"latitude":37.7749,"longitude":-122.4194}},"endLocation":{"latLng":{"latitude":37.8044,"longitude":-122.4108}}}]}]}`))
	}))
	defer server.Close()

	client := NewClientWithHTTP("test-key", server.Client(), server.URL)
	req := &routingpb.ComputeRoutesRequest{
		Origin:                   addr("123 Main St, SF"),
		Destination:              addr("1 Ferry Building, SF"),
		TravelMode:               routingpb.RouteTravelMode_DRIVE,
		RoutingPreference:        routingpb.RoutingPreference_TRAFFIC_AWARE,
		ComputeAlternativeRoutes: true,
	}
	resp, err := client.ComputeRoutes(context.Background(), req, RoutesFieldMask(false, false))
	if err != nil {
		t.Fatalf("ComputeRoutes returned error: %v", err)
	}
	if len(resp.GetRoutes()) != 1 {
		t.Fatalf("routes len = %d, want 1", len(resp.GetRoutes()))
	}
	if resp.GetRoutes()[0].GetDuration().GetSeconds() != 1440 {
		t.Fatalf("duration = %ds, want 1440", resp.GetRoutes()[0].GetDuration().GetSeconds())
	}
}

func TestComputeRoutesReturnsAPIStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer server.Close()

	client := NewClientWithHTTP("test-key", server.Client(), server.URL)
	_, err := client.ComputeRoutes(context.Background(), &routingpb.ComputeRoutesRequest{
		Origin: addr("a"), Destination: addr("b"), TravelMode: routingpb.RouteTravelMode_DRIVE,
	}, RoutesFieldMask(false, false))
	if err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("want error carrying API message, got %v", err)
	}
}

func TestRoutesFieldMask(t *testing.T) {
	full := RoutesFieldMask(true, true)
	for _, want := range []string{"tollInfo", "speedReadingIntervals", "encodedPolyline"} {
		if !strings.Contains(full, want) {
			t.Errorf("full mask missing %q: %s", want, full)
		}
	}
	if strings.Contains(RoutesFieldMask(false, false), "tollInfo") {
		t.Error("base mask should not include tollInfo")
	}
}
