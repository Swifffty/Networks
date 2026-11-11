package infolocation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"

	"places/asynchttp"
)

const weatherAPI = "https://api.openweathermap.org/data/2.5/weather"

type WeatherResponse struct {
	Weather []struct {
		Main        string `json:"main"`
		Description string `json:"description"`
	} `json:"weather"`

	Main struct {
		Temp     float64 `json:"temp"`
		Humidity int     `json:"humidity"`
	} `json:"main"`

	Wind struct {
		Speed float64 `json:"speed"`
	} `json:"wind"`

	Rain *struct {
		OneHour float64 `json:"1h"`
	} `json:"rain"`

	Clouds struct {
		All int `json:"all"`
	} `json:"clouds"`
}

func getWeatherAsync(ctx context.Context, client *asynchttp.Client, lat, lng float64,
	onDone func(WeatherResponse, error)) {

	key := os.Getenv("WEATHER_KEY")
	if key == "" {
		onDone(WeatherResponse{}, errors.New("weather: не задана переменная окружения WEATHER_KEY"))
		return
	}

	params := url.Values{}
	params.Set("lat", strconv.FormatFloat(lat, 'f', -1, 64))
	params.Set("lon", strconv.FormatFloat(lng, 'f', -1, 64))
	params.Set("appid", key)
	params.Set("lang", "ru")
	params.Set("units", "metric")

	client.Get(ctx, weatherAPI+"?"+params.Encode(), func(status int, body []byte, err error) {
		w, err := parseWeather(status, body, err)
		onDone(w, err)
	})
}

func parseWeather(status int, body []byte, err error) (WeatherResponse, error) {
	var data WeatherResponse
	if err != nil {
		return data, fmt.Errorf("weather: %w", err)
	}
	if status != 200 {
		return data, fmt.Errorf("weather: сервер вернул %d: %s", status, shorten(body))
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return data, fmt.Errorf("weather: разбор JSON: %w", err)
	}
	if len(data.Weather) < 1 {
		return data, errors.New("weather: не удалось узнать погоду")
	}
	return data, nil
}

func shorten(body []byte) string {
	if len(body) > 300 {
		return string(body[:300]) + "…"
	}
	return string(body)
}
