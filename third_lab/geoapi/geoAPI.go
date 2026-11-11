package geoapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"

	"places/asynchttp"
)

const (
	limitPlaces = "5"
	locale      = "ru"
	geocodeURL  = "https://graphhopper.com/api/1/geocode"
)

type geocodeResponse struct {
	Hits []Hit `json:"hits"`
}

type Hit struct {
	Point struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"point"`
	Name     string `json:"name"`
	Country  string `json:"country"`
	State    string `json:"state"`
	City     string `json:"city"`
	District string `json:"district"`
	OsmValue string `json:"osm_value"`
}

func SearchLocations(ctx context.Context, client *asynchttp.Client, place string, done func([]Hit, error)) {
	key := os.Getenv("GRAPHHOPPER_KEY")
	if key == "" {
		done(nil, errors.New("geocode: не задана переменная окружения GRAPHHOPPER_KEY"))
		return
	}

	params := url.Values{}
	params.Set("q", place)
	params.Set("locale", locale)
	params.Set("limit", limitPlaces)
	params.Set("key", key)

	client.Get(ctx, geocodeURL+"?"+params.Encode(), func(status int, body []byte, err error) {
		hits, err := parseLocations(status, body, err)
		done(hits, err)
	})
}

func parseLocations(status int, body []byte, err error) ([]Hit, error) {
	if err != nil {
		return nil, fmt.Errorf("geocode: %w", err)
	}
	if status != 200 {
		return nil, fmt.Errorf("geocode: сервер вернул %d: %s", status, shorten(body))
	}

	var data geocodeResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("geocode: разбор JSON: %w", err)
	}
	return data.Hits, nil
}

func shorten(body []byte) string {
	if len(body) > 300 {
		return string(body[:300]) + "…"
	}
	return string(body)
}
