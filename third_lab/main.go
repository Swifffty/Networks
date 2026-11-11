package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"places/asynchttp"
	"places/geoapi"
	"places/infolocation"
)

func main() {
	for _, key := range []string{"GRAPHHOPPER_KEY", "WEATHER_KEY"} {
		if os.Getenv(key) == "" {
			log.Fatalf("Задайте переменную окружения %s", key)
		}
	}

	client, err := asynchttp.New(15 * time.Second)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	a := app.New()
	w := a.NewWindow("Места")

	var (
		hits       []geoapi.Hit
		infoID     int
		cancelInfo context.CancelFunc
	)

	entry := widget.NewEntry()
	entry.SetPlaceHolder("Название места")

	status := widget.NewLabel("")

	resultBox := container.NewVBox()
	resultScroll := container.NewVScroll(resultBox)

	showResult := func(obj fyne.CanvasObject) {
		if obj == nil {
			resultBox.Objects = nil
		} else {
			resultBox.Objects = []fyne.CanvasObject{obj}
		}
		resultBox.Refresh()
		resultScroll.ScrollToTop()
	}

	list := widget.NewList(
		func() int { return len(hits) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			line := hitTitle(hits[id])
			if sub := hitSubtitle(hits[id]); sub != "" {
				line += " — " + sub
			}
			o.(*widget.Label).SetText(line)
		},
	)

	stopInfo := func() {
		if cancelInfo != nil {
			cancelInfo()
			cancelInfo = nil
		}
		infoID++
	}

	list.OnSelected = func(id widget.ListItemID) {
		h := hits[id]

		stopInfo()
		ctx, cancel := context.WithCancel(context.Background())
		cancelInfo = cancel
		myID := infoID

		status.SetText(fmt.Sprintf("Загружаем информацию: %s...", hitTitle(h)))
		showResult(nil)

		infolocation.GetInfoLocation(ctx, client, h.Point.Lat, h.Point.Lng,
			func(info infolocation.LocationInfo) {
				fyne.Do(func() {
					if myID != infoID {
						return
					}
					cancel()
					cancelInfo = nil

					status.SetText("Готово")
					showResult(buildResult(h, info))
				})
			})
	}

	var button *widget.Button

	search := func() {
		query := strings.TrimSpace(entry.Text)
		if query == "" || button.Disabled() {
			return
		}

		stopInfo()
		button.Disable()
		status.SetText("Поиск...")
		showResult(nil)
		hits = nil
		list.UnselectAll()
		list.Refresh()

		geoapi.SearchLocations(context.Background(), client, query, func(found []geoapi.Hit, err error) {
			fyne.Do(func() {
				button.Enable()
				switch {
				case err != nil:
					status.SetText("Ошибка: " + err.Error())
				case len(found) == 0:
					status.SetText("Ничего не найдено")
				default:
					hits = dedupeHits(found)
					status.SetText("Выберите место:")
				}
				list.Refresh()
			})
		})
	}

	button = widget.NewButton("Найти", search)
	entry.OnSubmitted = func(string) { search() }

	closeButton := widget.NewButtonWithIcon("", theme.CancelIcon(), a.Quit)

	top := container.NewVBox(
		container.NewBorder(nil, nil, nil, container.NewHBox(button, closeButton), entry),
		status,
	)
	split := container.NewVSplit(list, resultScroll)
	split.Offset = 0.3

	w.SetContent(container.NewBorder(top, nil, nil, nil, split))
	w.Resize(fyne.NewSize(720, 760))
	w.ShowAndRun()
}
