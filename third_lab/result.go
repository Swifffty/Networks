package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"places/geoapi"
	"places/infolocation"
)

func buildResult(h geoapi.Hit, info infolocation.LocationInfo) fyne.CanvasObject {
	items := []fyne.CanvasObject{
		boldLabel(hitTitle(h)),
		wrapLabel(hitSubtitle(h)),
		widget.NewSeparator(),
		boldLabel("Погода"),
		wrapLabel(formatWeather(info)),
		widget.NewSeparator(),
		boldLabel("Интересные места рядом"),
	}

	switch {
	case info.ErrNearby != nil:
		items = append(items, wrapLabel(fmt.Sprintf("Не удалось получить места: %v", info.ErrNearby)))
	case len(info.Nearby) == 0:
		items = append(items, wrapLabel("Рядом ничего не найдено."))
	default:
		for i, p := range info.Nearby {
			items = append(items, boldLabel(fmt.Sprintf("%d. %s (%d м)", i+1, p.Title, p.Dist)))
			if len(p.Image) > 0 {
				items = append(items, placeImage(p))
			}
			items = append(items, wrapLabel(p.Description))
		}
	}

	return container.NewVBox(items...)
}

func placeImage(p infolocation.DescriptionPlace) fyne.CanvasObject {
	res := fyne.NewStaticResource(fmt.Sprintf("wiki_%d", p.PageID), p.Image)
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(260, 180))
	return container.NewHBox(img)
}

func boldLabel(text string) *widget.Label {
	return widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}

func wrapLabel(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	return l
}
