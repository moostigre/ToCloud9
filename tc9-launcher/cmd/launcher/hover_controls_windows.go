//go:build windows

package main

import (
	_ "embed"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

//go:embed assets/fontawesome/rotate.svg
var faRotate []byte

//go:embed assets/fontawesome/circle-info.svg
var faCircleInfo []byte

//go:embed assets/swp-icons/news-scroll.svg
var swpNewsIcon []byte

//go:embed assets/swp-icons/client-portal.svg
var swpClientIcon []byte

//go:embed assets/swp-icons/accounts-helm.svg
var swpAccountsIcon []byte

//go:embed assets/swp-icons/config-rune.svg
var swpConfigurationIcon []byte

//go:embed assets/swp-icons/addons-tome.svg
var swpAddonsIcon []byte

func fontAwesomeResource(name string, source []byte, colour string) fyne.Resource {
	return fyne.NewStaticResource(name, []byte(strings.ReplaceAll(string(source), "currentColor", colour)))
}

// hoverIconButton keeps refresh actions visually quiet until the pointer is
// over them, where it uses the same gold/black treatment as primary actions.
type hoverIconButton struct {
	widget.BaseWidget
	OnTapped func()
	hovered  bool
	disabled bool
}

func newHoverIconButton(tapped func()) *hoverIconButton {
	button := &hoverIconButton{OnTapped: tapped}
	button.ExtendBaseWidget(button)
	return button
}

func (button *hoverIconButton) CreateRenderer() fyne.WidgetRenderer {
	background := canvas.NewRectangle(color.Transparent)
	goldIcon := canvas.NewImageFromResource(fontAwesomeResource("fa-rotate-gold.svg", faRotate, "#e4b148"))
	blackIcon := canvas.NewImageFromResource(fontAwesomeResource("fa-rotate-black.svg", faRotate, "#000000"))
	disabledIcon := canvas.NewImageFromResource(fontAwesomeResource("fa-rotate-disabled.svg", faRotate, "#969ea5"))
	for _, icon := range []*canvas.Image{goldIcon, blackIcon, disabledIcon} {
		icon.FillMode = canvas.ImageFillContain
	}
	blackIcon.Hide()
	disabledIcon.Hide()
	return &hoverIconButtonRenderer{button: button, background: background, goldIcon: goldIcon, blackIcon: blackIcon, disabledIcon: disabledIcon, objects: []fyne.CanvasObject{background, goldIcon, blackIcon, disabledIcon}}
}
func (button *hoverIconButton) Tapped(*fyne.PointEvent) {
	if !button.disabled && button.OnTapped != nil {
		button.OnTapped()
	}
}
func (button *hoverIconButton) MouseIn(*desktop.MouseEvent) {
	if !button.disabled {
		button.hovered = true
		button.Refresh()
	}
}
func (button *hoverIconButton) MouseMoved(*desktop.MouseEvent) {}
func (button *hoverIconButton) MouseOut()                      { button.hovered = false; button.Refresh() }
func (button *hoverIconButton) Cursor() desktop.Cursor         { return desktop.PointerCursor }
func (button *hoverIconButton) Enable()                        { button.disabled = false; button.Refresh() }
func (button *hoverIconButton) Disable() {
	button.disabled = true
	button.hovered = false
	button.Refresh()
}

type hoverIconButtonRenderer struct {
	button                            *hoverIconButton
	background                        *canvas.Rectangle
	goldIcon, blackIcon, disabledIcon *canvas.Image
	objects                           []fyne.CanvasObject
}

func (renderer *hoverIconButtonRenderer) Layout(size fyne.Size) {
	renderer.background.Resize(size)
	inset := float32(8)
	for _, icon := range []*canvas.Image{renderer.goldIcon, renderer.blackIcon, renderer.disabledIcon} {
		icon.Move(fyne.NewPos(inset, inset))
		icon.Resize(fyne.NewSize(size.Width-2*inset, size.Height-2*inset))
	}
}
func (renderer *hoverIconButtonRenderer) MinSize() fyne.Size { return fyne.NewSquareSize(32) }
func (renderer *hoverIconButtonRenderer) Refresh() {
	if renderer.button.disabled {
		renderer.background.FillColor = color.Transparent
		renderer.goldIcon.Hide()
		renderer.blackIcon.Hide()
		renderer.disabledIcon.Show()
	} else if renderer.button.hovered {
		renderer.background.FillColor = gold
		renderer.goldIcon.Hide()
		renderer.disabledIcon.Hide()
		renderer.blackIcon.Show()
	} else {
		renderer.background.FillColor = color.Transparent
		renderer.blackIcon.Hide()
		renderer.disabledIcon.Hide()
		renderer.goldIcon.Show()
	}
	canvas.Refresh(renderer.background)
	canvas.Refresh(renderer.goldIcon)
	canvas.Refresh(renderer.blackIcon)
	canvas.Refresh(renderer.disabledIcon)
}
func (renderer *hoverIconButtonRenderer) Objects() []fyne.CanvasObject { return renderer.objects }
func (renderer *hoverIconButtonRenderer) Destroy()                     {}

type hoverTabButton struct {
	widget.BaseWidget
	Text                string
	IconGold, IconBlack fyne.Resource
	OnTapped            func()
	hovered, selected   bool
}

func newHoverTabButton(text string, icon []byte, tapped func()) *hoverTabButton {
	button := &hoverTabButton{Text: text, IconGold: fontAwesomeResource("fa-"+strings.ToLower(text)+"-gold.svg", icon, "#e4b148"), IconBlack: fontAwesomeResource("fa-"+strings.ToLower(text)+"-black.svg", icon, "#000000"), OnTapped: tapped}
	button.ExtendBaseWidget(button)
	return button
}
func (button *hoverTabButton) CreateRenderer() fyne.WidgetRenderer {
	background := canvas.NewRectangle(tabGlass)
	goldIcon := canvas.NewImageFromResource(button.IconGold)
	goldIcon.FillMode = canvas.ImageFillContain
	blackIcon := canvas.NewImageFromResource(button.IconBlack)
	blackIcon.FillMode = canvas.ImageFillContain
	blackIcon.Hide()
	label := canvas.NewText(button.Text, gold)
	label.TextStyle.Bold = true
	label.TextSize = 10
	label.Alignment = fyne.TextAlignCenter
	activeBorder := canvas.NewRectangle(color.Transparent)
	if button.selected {
		activeBorder.FillColor = gold
		background.FillColor = color.NRGBA{R: 24, G: 18, B: 31, A: 250}
	}
	return &hoverTabRenderer{button: button, background: background, goldIcon: goldIcon, blackIcon: blackIcon, label: label, activeBorder: activeBorder, objects: []fyne.CanvasObject{background, goldIcon, blackIcon, label, activeBorder}}
}
func (button *hoverTabButton) Tapped(*fyne.PointEvent) {
	if button.OnTapped != nil {
		button.OnTapped()
	}
}
func (button *hoverTabButton) MouseIn(*desktop.MouseEvent)    { button.hovered = true; button.Refresh() }
func (button *hoverTabButton) MouseMoved(*desktop.MouseEvent) {}
func (button *hoverTabButton) MouseOut()                      { button.hovered = false; button.Refresh() }
func (button *hoverTabButton) Cursor() desktop.Cursor         { return desktop.PointerCursor }

type hoverTabRenderer struct {
	button              *hoverTabButton
	background          *canvas.Rectangle
	goldIcon, blackIcon *canvas.Image
	label               *canvas.Text
	activeBorder        *canvas.Rectangle
	objects             []fyne.CanvasObject
}

func (renderer *hoverTabRenderer) Layout(size fyne.Size) {
	renderer.background.Move(fyne.NewPos(0, 0))
	renderer.background.Resize(size)
	iconSize := float32(21)
	labelSize := renderer.label.MinSize()
	x := (size.Width - iconSize) / 2
	for _, icon := range []*canvas.Image{renderer.goldIcon, renderer.blackIcon} {
		icon.Move(fyne.NewPos(x, 12))
		icon.Resize(fyne.NewSquareSize(iconSize))
	}
	renderer.label.Move(fyne.NewPos(0, 39))
	renderer.label.Resize(fyne.NewSize(size.Width, labelSize.Height))
	renderer.activeBorder.Move(fyne.NewPos(0, size.Height-3))
	renderer.activeBorder.Resize(fyne.NewSize(size.Width, 3))
}
func (renderer *hoverTabRenderer) MinSize() fyne.Size {
	return fyne.NewSize(renderer.label.MinSize().Width+34, 68)
}
func (renderer *hoverTabRenderer) Refresh() {
	hovered := renderer.button.hovered
	if hovered {
		renderer.background.FillColor = color.NRGBA{R: 31, G: 24, B: 35, A: 230}
		renderer.label.Color = white
		renderer.goldIcon.Hide()
		renderer.blackIcon.Hide()
		renderer.goldIcon.Show()
	} else {
		renderer.background.FillColor = color.NRGBA{R: 8, G: 10, B: 15, A: 155}
		renderer.label.Color = gold
		renderer.blackIcon.Hide()
		renderer.goldIcon.Show()
	}
	if renderer.button.selected {
		renderer.background.FillColor = color.NRGBA{R: 24, G: 18, B: 31, A: 245}
		renderer.activeBorder.FillColor = gold
	} else {
		renderer.activeBorder.FillColor = color.Transparent
	}
	canvas.Refresh(renderer.background)
	canvas.Refresh(renderer.goldIcon)
	canvas.Refresh(renderer.blackIcon)
	canvas.Refresh(renderer.label)
	canvas.Refresh(renderer.activeBorder)
}
func (renderer *hoverTabRenderer) Objects() []fyne.CanvasObject { return renderer.objects }
func (renderer *hoverTabRenderer) Destroy()                     {}
