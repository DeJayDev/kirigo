package commute

import (
	"context"
	"errors"
	"fmt"
	"strings"

	placespb "cloud.google.com/go/maps/places/apiv1/placespb"

	"github.com/DeJayDev/kirigo/internal/googlemaps"
)

type placesClient interface {
	SearchText(ctx context.Context, req *placespb.SearchTextRequest, fieldMask string) (*placespb.SearchTextResponse, error)
}

type SearchResult struct {
	Status string       `json:"status"`
	Query  string       `json:"query"`
	Places []FoundPlace `json:"places"`
}

type FoundPlace struct {
	Name    string  `json:"name"`
	Address string  `json:"address"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	PlaceID string  `json:"place_id"`
}

// Search runs a Places text search; near (a place name or address) is appended
// to the query as "near <address>".
func Search(ctx context.Context, client placesClient, query, near string, places map[string]string) (SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchResult{}, errors.New("query is required")
	}
	if near = expandPlace(places, near); near != "" {
		query = fmt.Sprintf("%s near %s", query, near)
	}

	response, err := client.SearchText(ctx, &placespb.SearchTextRequest{TextQuery: query, MaxResultCount: 5}, googlemaps.PlacesFieldMask)
	if err != nil {
		return SearchResult{}, err
	}

	found := make([]FoundPlace, 0, len(response.GetPlaces()))
	for _, p := range response.GetPlaces() {
		found = append(found, FoundPlace{
			Name:    p.GetDisplayName().GetText(),
			Address: p.GetFormattedAddress(),
			Lat:     roundCoordinate(p.GetLocation().GetLatitude()),
			Lng:     roundCoordinate(p.GetLocation().GetLongitude()),
			PlaceID: p.GetId(),
		})
	}
	return SearchResult{Status: "ok", Query: query, Places: found}, nil
}
