package infolocation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"

	"places/asynchttp"
)

const (
	searchRadius  = "3000"
	searchLimit   = "10"
	extractChars  = "500"
	thumbSize     = "300"
	noDescription = "Описание недоступно"
	wikiAPI       = "https://ru.wikipedia.org/w/api.php"
)

type NearbyPlace struct {
	PageID int     `json:"pageid"`
	Title  string  `json:"title"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Dist   float64 `json:"dist"`
}

type geoSearchResponse struct {
	Query struct {
		GeoSearch []NearbyPlace `json:"geosearch"`
	} `json:"query"`
	Error *struct {
		Code string `json:"code"`
		Info string `json:"info"`
	} `json:"error"`
}

type extractResponse struct {
	Query struct {
		Pages []struct {
			PageID    int    `json:"pageid"`
			Title     string `json:"title"`
			Extract   string `json:"extract"`
			Missing   bool   `json:"missing"`
			Invalid   bool   `json:"invalid"`
			Thumbnail *struct {
				Source string `json:"source"`
			} `json:"thumbnail"`
		} `json:"pages"`
	} `json:"query"`
	Error *struct {
		Code string `json:"code"`
		Info string `json:"info"`
	} `json:"error"`
}

type DescriptionPlace struct {
	PageID      int
	Title       string
	Dist        int
	Description string
	Image       []byte
}

func getNearbyAsync(ctx context.Context, client *asynchttp.Client, lat, lng float64,
	onDone func([]DescriptionPlace, error)) {

	params := url.Values{}
	params.Set("action", "query")
	params.Set("list", "geosearch")
	params.Set("gscoord", strconv.FormatFloat(lat, 'f', -1, 64)+"|"+strconv.FormatFloat(lng, 'f', -1, 64))
	params.Set("gsradius", searchRadius)
	params.Set("gslimit", searchLimit)
	params.Set("format", "json")
	params.Set("formatversion", "2")

	client.Get(ctx, wikiAPI+"?"+params.Encode(), func(status int, body []byte, err error) {
		nearby, err := parseNearby(status, body, err)
		if err != nil {
			onDone(nil, err)
			return
		}
		if len(nearby) == 0 {
			onDone(nil, nil)
			return
		}

		places := make([]DescriptionPlace, len(nearby))
		var pending atomic.Int32
		pending.Store(int32(len(nearby)))

		for i, p := range nearby {
			getDescriptionAsync(ctx, client, p, func(dp DescriptionPlace) {
				places[i] = dp
				if pending.Add(-1) == 0 {
					onDone(places, nil)
				}
			})
		}
	})
}

func getDescriptionAsync(ctx context.Context, client *asynchttp.Client, p NearbyPlace,
	onDone func(DescriptionPlace)) {

	dp := DescriptionPlace{PageID: p.PageID, Title: p.Title, Dist: int(p.Dist)}

	getExtractAsync(ctx, client, p.PageID, func(text, imageURL string, err error) {
		if err != nil {
			dp.Description = noDescription
			onDone(dp)
			return
		}
		dp.Description = text
		if imageURL == "" {
			onDone(dp)
			return
		}
		getImageAsync(ctx, client, imageURL, func(img []byte) {
			dp.Image = img
			onDone(dp)
		})
	})
}

func getExtractAsync(ctx context.Context, client *asynchttp.Client, pageID int,
	onDone func(text, imageURL string, err error)) {

	params := url.Values{}
	params.Set("action", "query")
	params.Set("prop", "extracts|pageimages")
	params.Set("pageids", strconv.Itoa(pageID))
	params.Set("exintro", "1")
	params.Set("explaintext", "1")
	params.Set("exchars", extractChars)
	params.Set("piprop", "thumbnail")
	params.Set("pithumbsize", thumbSize)
	params.Set("format", "json")
	params.Set("formatversion", "2")

	client.Get(ctx, wikiAPI+"?"+params.Encode(), func(status int, body []byte, err error) {
		text, imageURL, err := parseExtract(status, body, err)
		onDone(text, imageURL, err)
	})
}

func getImageAsync(ctx context.Context, client *asynchttp.Client, imageURL string, onDone func([]byte)) {
	client.Get(ctx, imageURL, func(status int, body []byte, err error) {
		if err != nil || status != 200 {
			onDone(nil)
			return
		}
		switch http.DetectContentType(body) {
		case "image/jpeg", "image/png":
			onDone(body)
		default:
			onDone(nil)
		}
	})
}

func parseNearby(status int, body []byte, err error) ([]NearbyPlace, error) {
	if err != nil {
		return nil, fmt.Errorf("geosearch: %w", err)
	}
	if status != 200 {
		return nil, fmt.Errorf("geosearch: сервер вернул %d: %s", status, shorten(body))
	}
	var data geoSearchResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("geosearch: разбор JSON: %w", err)
	}
	if data.Error != nil {
		return nil, fmt.Errorf("geosearch: ошибка API %s: %s", data.Error.Code, data.Error.Info)
	}
	return data.Query.GeoSearch, nil
}

func parseExtract(status int, body []byte, err error) (text, imageURL string, _ error) {
	if err != nil {
		return "", "", fmt.Errorf("extracts: %w", err)
	}
	if status != 200 {
		return "", "", fmt.Errorf("extracts: сервер вернул %d: %s", status, shorten(body))
	}
	var data extractResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return "", "", fmt.Errorf("extracts: разбор JSON: %w", err)
	}
	if data.Error != nil {
		return "", "", fmt.Errorf("extracts: ошибка API %s: %s", data.Error.Code, data.Error.Info)
	}
	if len(data.Query.Pages) == 0 {
		return noDescription, "", nil
	}

	page := data.Query.Pages[0]
	text = strings.TrimSpace(page.Extract)
	if page.Missing || page.Invalid || text == "" {
		text = noDescription
	}
	if page.Thumbnail != nil {
		imageURL = page.Thumbnail.Source
	}
	return text, imageURL, nil
}
