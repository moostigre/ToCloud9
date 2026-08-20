//go:build windows

package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type inputTheme struct{ fyne.Theme }

func (inputTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameForeground:
		return color.Black
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 105, G: 110, B: 116, A: 255}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 228, G: 177, B: 72, A: 150}
	}
	return swpTheme{theme.DefaultTheme()}.Color(name, variant)
}

func blackInput(entry *widget.Entry) fyne.CanvasObject {
	return container.NewThemeOverride(entry, inputTheme{theme.DefaultTheme()})
}
