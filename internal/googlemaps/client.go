// Package googlemaps is a thin transport over the Google Routes and Places REST
// endpoints. Requests and responses use the official routingpb/placespb types
// (proto3 JSON via protojson), so the API contract is generated, not hand-maintained.
package googlemaps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	placespb "cloud.google.com/go/maps/places/apiv1/placespb"
	routingpb "cloud.google.com/go/maps/routing/apiv2/routingpb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	routesEndpoint = "https://routes.googleapis.com/directions/v2:computeRoutes"
	placesEndpoint = "https://places.googleapis.com/v1/places:searchText"
)

type Client struct {
	apiKey         string
	httpClient     *http.Client
	routesEndpoint string
	placesEndpoint string
}

func NewClient(apiKey string) *Client {
	return &Client{apiKey: apiKey, httpClient: http.DefaultClient, routesEndpoint: routesEndpoint, placesEndpoint: placesEndpoint}
}

// NewClientWithHTTP points every API call at endpoint (a test server).
func NewClientWithHTTP(apiKey string, httpClient *http.Client, endpoint string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{apiKey: apiKey, httpClient: httpClient, routesEndpoint: endpoint, placesEndpoint: endpoint}
}

// ComputeRoutes POSTs req to the Routes REST endpoint and decodes the response.
// fieldMask is the X-Goog-FieldMask selecting which response fields to return.
func (c *Client) ComputeRoutes(ctx context.Context, req *routingpb.ComputeRoutesRequest, fieldMask string) (*routingpb.ComputeRoutesResponse, error) {
	var out routingpb.ComputeRoutesResponse
	if err := c.post(ctx, "Routes API", c.routesEndpoint, fieldMask, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchText POSTs req to the Places (New) Text Search endpoint.
func (c *Client) SearchText(ctx context.Context, req *placespb.SearchTextRequest, fieldMask string) (*placespb.SearchTextResponse, error) {
	var out placespb.SearchTextResponse
	if err := c.post(ctx, "Places API", c.placesEndpoint, fieldMask, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) post(ctx context.Context, api, endpoint, fieldMask string, req, out proto.Message) error {
	body, err := protojson.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", api, err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create %s request: %w", api, err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("X-Goog-Api-Key", c.apiKey)
	httpRequest.Header.Set("X-Goog-FieldMask", fieldMask)

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("call %s: %w", api, err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read %s response: %w", api, err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		if msg := apiErrorMessage(payload); msg != "" {
			return fmt.Errorf("unexpected HTTP %s from %s: %s", response.Status, api, msg)
		}
		return fmt.Errorf("unexpected HTTP %s from %s", response.Status, api)
	}

	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode %s response: %w", api, err)
	}
	return nil
}

// RoutesFieldMask builds the response field mask. Toll and traffic-segment fields
// are only meaningful for driving, and transit step details only for transit, so
// callers pass false for other modes.
func RoutesFieldMask(tolls, segments, transit bool) string {
	mask := "geocodingResults,routes.description,routes.duration,routes.staticDuration,routes.distanceMeters," +
		"routes.legs.duration,routes.legs.staticDuration,routes.legs.distanceMeters," +
		"routes.legs.startLocation,routes.legs.endLocation"
	if tolls {
		mask += ",routes.travelAdvisory.tollInfo"
	}
	if segments {
		mask += ",routes.polyline.encodedPolyline,routes.travelAdvisory.speedReadingIntervals,routes.legs.travelAdvisory.speedReadingIntervals," +
			"routes.legs.steps.navigationInstruction.instructions"
	}
	if transit {
		mask += ",routes.legs.steps.travelMode,routes.legs.steps.staticDuration,routes.legs.steps.transitDetails"
	}
	return mask
}

const PlacesFieldMask = "places.id,places.displayName,places.formattedAddress,places.location"

// apiErrorMessage pulls the human message out of Google's {"error":{...}} envelope.
func apiErrorMessage(payload []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(payload, &envelope)
	return envelope.Error.Message
}

func FormatDuration(seconds int) string {
	if seconds < 60 {
		return strconv.Itoa(seconds) + " secs"
	}
	minutes := (seconds + 30) / 60
	if minutes < 60 {
		if minutes == 1 {
			return "1 min"
		}
		return strconv.Itoa(minutes) + " mins"
	}
	hours := minutes / 60
	remainingMinutes := minutes % 60
	if remainingMinutes == 0 {
		if hours == 1 {
			return "1 hour"
		}
		return strconv.Itoa(hours) + " hours"
	}
	if hours == 1 {
		return fmt.Sprintf("1 hour %d mins", remainingMinutes)
	}
	return fmt.Sprintf("%d hours %d mins", hours, remainingMinutes)
}
