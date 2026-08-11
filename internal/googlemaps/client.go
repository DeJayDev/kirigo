// Package googlemaps is a thin transport over the Google Routes REST endpoint.
// Requests and responses use the official routingpb types (proto3 JSON via
// protojson), so the API contract is generated, not hand-maintained.
package googlemaps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	routingpb "cloud.google.com/go/maps/routing/apiv2/routingpb"
	"google.golang.org/protobuf/encoding/protojson"
)

const routesEndpoint = "https://routes.googleapis.com/directions/v2:computeRoutes"

type Client struct {
	apiKey     string
	httpClient *http.Client
	endpoint   string
}

func NewClient(apiKey string) *Client {
	return &Client{apiKey: apiKey, httpClient: http.DefaultClient, endpoint: routesEndpoint}
}

func NewClientWithHTTP(apiKey string, httpClient *http.Client, endpoint string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if endpoint == "" {
		endpoint = routesEndpoint
	}
	return &Client{apiKey: apiKey, httpClient: httpClient, endpoint: endpoint}
}

// ComputeRoutes POSTs req to the Routes REST endpoint and decodes the response.
// fieldMask is the X-Goog-FieldMask selecting which response fields to return.
func (c *Client) ComputeRoutes(ctx context.Context, req *routingpb.ComputeRoutesRequest, fieldMask string) (*routingpb.ComputeRoutesResponse, error) {
	body, err := protojson.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode Routes API request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Routes API request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("X-Goog-Api-Key", c.apiKey)
	httpRequest.Header.Set("X-Goog-FieldMask", fieldMask)

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("call Routes API: %w", err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read Routes API response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		if msg := apiErrorMessage(payload); msg != "" {
			return nil, fmt.Errorf("unexpected HTTP %s from Routes API: %s", response.Status, msg)
		}
		return nil, fmt.Errorf("unexpected HTTP %s from Routes API", response.Status)
	}

	var out routingpb.ComputeRoutesResponse
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("decode Routes API response: %w", err)
	}
	return &out, nil
}

// RoutesFieldMask builds the response field mask. Toll and traffic-segment fields
// are only meaningful for driving, so callers pass false for other modes.
func RoutesFieldMask(tolls, segments bool) string {
	mask := "routes.description,routes.duration,routes.staticDuration,routes.distanceMeters," +
		"routes.legs.duration,routes.legs.staticDuration,routes.legs.distanceMeters," +
		"routes.legs.startLocation,routes.legs.endLocation"
	if tolls {
		mask += ",routes.travelAdvisory.tollInfo"
	}
	if segments {
		mask += ",routes.polyline.encodedPolyline,routes.travelAdvisory.speedReadingIntervals,routes.legs.travelAdvisory.speedReadingIntervals"
	}
	return mask
}

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
