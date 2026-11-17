package main

import (
	"fmt"
	"strings"

	"places/geoapi"
	"places/infolocation"
)

func formatWeather(info infolocation.LocationInfo) string {
	if info.ErrWeather != nil {
		return fmt.Sprintf("Не удалось получить погоду: %v", info.ErrWeather)
	}

	w := info.Weather
	var b strings.Builder
	if len(w.Weather) > 0 {
		desc := w.Weather[0].Description
		if desc == "" {
			desc = w.Weather[0].Main
		}
		fmt.Fprintf(&b, "%s\n", desc)
	}
	fmt.Fprintf(&b, "Температура: %.1f °C\n", w.Main.Temp)
	fmt.Fprintf(&b, "Влажность: %d%%\n", w.Main.Humidity)
	fmt.Fprintf(&b, "Давление: %.0f мм рт. ст. (%d гПа)\n", float64(w.Main.Pressure)*0.750062, w.Main.Pressure)
	fmt.Fprintf(&b, "Ветер: %.1f м/с\n", w.Wind.Speed)
	fmt.Fprintf(&b, "Облачность: %d%%", w.Clouds.All)
	if w.Rain != nil {
		fmt.Fprintf(&b, "\nДождь: %.1f мм за час", w.Rain.OneHour)
	}
	return b.String()
}

func hitTitle(h geoapi.Hit) string {
	if h.Name != "" {
		return h.Name
	}
	return "Без названия"
}

func hitSubtitle(h geoapi.Hit) string {
	seen := map[string]bool{h.Name: true}
	parts := make([]string, 0, 4)
	for _, s := range []string{h.District, h.City, h.State, h.Country} {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		parts = append(parts, s)
	}

	sub := strings.Join(parts, ", ")
	if t := osmTypeName(h.OsmValue); t != "" {
		if sub != "" {
			sub += " · "
		}
		sub += t
	}
	return sub
}

func dedupeHits(hits []geoapi.Hit) []geoapi.Hit {
	seen := make(map[string]bool, len(hits))
	out := make([]geoapi.Hit, 0, len(hits))
	for _, h := range hits {
		key := hitTitle(h) + "\x00" + hitSubtitle(h)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, h)
	}
	return out
}

func osmTypeName(v string) string {
	switch v {
	case "city", "town":
		return "город"
	case "village":
		return "село"
	case "hamlet":
		return "деревня"
	case "suburb", "quarter", "neighbourhood":
		return "район"
	case "residential", "living_street", "unclassified", "tertiary",
		"secondary", "primary", "pedestrian", "service", "road":
		return "улица"
	case "square":
		return "площадь"
	case "station":
		return "станция"
	case "museum":
		return "музей"
	case "university":
		return "университет"
	case "park":
		return "парк"
	case "theatre":
		return "театр"
	case "hospital":
		return "больница"
	case "house":
		return "дом"
	case "bus_stop", "platform", "stop_position":
		return "остановка"
	default:
		return ""
	}
}
