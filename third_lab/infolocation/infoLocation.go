package infolocation

import (
	"context"
	"sync/atomic"

	"places/asynchttp"
)

type LocationInfo struct {
	Weather    WeatherResponse
	Nearby     []DescriptionPlace
	ErrWeather error
	ErrNearby  error
}

func GetInfoLocation(ctx context.Context, client *asynchttp.Client, lat float64, lng float64,
	done func(LocationInfo)) {

	var result LocationInfo

	var flag atomic.Int32
	flag.Store(2)

	getWeatherAsync(ctx, client, lat, lng, func(w WeatherResponse, err error) {
		result.Weather, result.ErrWeather = w, err
		if flag.Add(-1) == 0 {
			done(result)
		}
	})
	getNearbyAsync(ctx, client, lat, lng, func(desc []DescriptionPlace, err error) {
		result.Nearby, result.ErrNearby = desc, err
		if flag.Add(-1) == 0 {
			done(result)
		}
	})
}
