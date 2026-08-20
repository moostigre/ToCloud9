//go:build windows

package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// borderedButton is deliberately canvas-based: Fyne's standard high-importance
// button uses rounded corners, while the launcher artwork calls for a sharp,
// framed Warcraft-style action button.
type borderedButton struct {
	widget.BaseWidget
	Text         string
	OnTapped     func()
	Dropdown     bool
	DropdownOpen bool
	hovered      bool
	disabled     bool
	Primary      bool
}

func newBorderedButton(text string, tapped func()) *borderedButton {
	button := &borderedButton{Text: text, OnTapped: tapped}
	button.ExtendBaseWidget(button)
	return button
}

func (button *borderedButton) CreateRenderer() fyne.WidgetRenderer {
	outer := canvas.NewRectangle(gold)
	inner := canvas.NewRectangle(navy)
	label := canvas.NewText(button.Text, white)
	chevronLeft := canvas.NewLine(color.Transparent)
	chevronRight := canvas.NewLine(color.Transparent)
	chevronLeft.StrokeWidth = 2
	chevronRight.StrokeWidth = 2
	label.Alignment = fyne.TextAlignCenter
	label.TextStyle.Bold = true
	label.TextSize = 13
	return &borderedButtonRenderer{button: button, outer: outer, inner: inner, label: label, chevronLeft: chevronLeft, chevronRight: chevronRight, objects: []fyne.CanvasObject{outer, inner, label, chevronLeft, chevronRight}}
}

func (button *borderedButton) Tapped(*fyne.PointEvent) {
	if !button.disabled && button.OnTapped != nil {
		button.OnTapped()
	}
}

func (button *borderedButton) MouseIn(*desktop.MouseEvent) {
	if !button.disabled {
		button.hovered = true
		button.Refresh()
	}
}

func (button *borderedButton) MouseOut() {
	button.hovered = false
	button.Refresh()
}

func (button *borderedButton) MouseMoved(*desktop.MouseEvent) {}
func (button *borderedButton) Cursor() desktop.Cursor         { return desktop.PointerCursor }

func (button *borderedButton) Enable() {
	button.disabled = false
	button.Refresh()
}

func (button *borderedButton) Disable() {
	button.disabled = true
	button.hovered = false
	button.Refresh()
}

func (button *borderedButton) Disabled() bool { return button.disabled }

type borderedButtonRenderer struct {
	button       *borderedButton
	outer        *canvas.Rectangle
	inner        *canvas.Rectangle
	label        *canvas.Text
	chevronLeft  *canvas.Line
	chevronRight *canvas.Line
	objects      []fyne.CanvasObject
}

func (renderer *borderedButtonRenderer) Layout(size fyne.Size) {
	renderer.outer.Resize(size)
	renderer.inner.Move(fyne.NewPos(3, 3))
	renderer.inner.Resize(fyne.NewSize(size.Width-6, size.Height-6))
	labelHeight := renderer.label.MinSize().Height
	renderer.label.Move(fyne.NewPos(0, (size.Height-labelHeight)/2))
	renderer.label.Resize(fyne.NewSize(size.Width, labelHeight))
	renderer.layoutChevron(size)
}

func (renderer *borderedButtonRenderer) layoutChevron(size fyne.Size) {
	chevronX := size.Width - 22
	chevronY := (size.Height - 5) / 2
	if renderer.button.DropdownOpen {
		renderer.chevronLeft.Position1 = fyne.NewPos(chevronX, chevronY+5)
		renderer.chevronLeft.Position2 = fyne.NewPos(chevronX+5, chevronY)
		renderer.chevronRight.Position1 = fyne.NewPos(chevronX+5, chevronY)
		renderer.chevronRight.Position2 = fyne.NewPos(chevronX+10, chevronY+5)
		return
	}
	renderer.chevronLeft.Position1 = fyne.NewPos(chevronX, chevronY)
	renderer.chevronLeft.Position2 = fyne.NewPos(chevronX+5, chevronY+5)
	renderer.chevronRight.Position1 = fyne.NewPos(chevronX+5, chevronY+5)
	renderer.chevronRight.Position2 = fyne.NewPos(chevronX+10, chevronY)
}

func (renderer *borderedButtonRenderer) MinSize() fyne.Size {
	textSize := renderer.label.MinSize()
	return fyne.NewSize(textSize.Width+36, 42)
}

func (renderer *borderedButtonRenderer) Refresh() {
	renderer.label.Text = renderer.button.Text
	renderer.layoutChevron(renderer.button.Size())
	if renderer.button.disabled {
		renderer.outer.FillColor = color.NRGBA{R: 104, G: 108, B: 112, A: 255}
		renderer.inner.FillColor = navy
		renderer.label.Color = color.NRGBA{R: 150, G: 158, B: 165, A: 255}
		renderer.chevronLeft.StrokeColor = color.NRGBA{R: 150, G: 158, B: 165, A: 255}
		renderer.chevronRight.StrokeColor = color.NRGBA{R: 150, G: 158, B: 165, A: 255}
	} else if renderer.button.hovered {
		accent := gold
		if renderer.button.Primary {
			accent = purple
		}
		renderer.outer.FillColor = accent
		renderer.inner.FillColor = accent
		renderer.label.Color = color.Black
		renderer.chevronLeft.StrokeColor = color.Black
		renderer.chevronRight.StrokeColor = color.Black
	} else {
		renderer.outer.FillColor = gold
		renderer.inner.FillColor = navy
		if renderer.button.Primary {
			renderer.outer.FillColor = purple
			renderer.inner.FillColor = color.NRGBA{R: 55, G: 25, B: 68, A: 255}
		}
		renderer.label.Color = white
		renderer.chevronLeft.StrokeColor = white
		renderer.chevronRight.StrokeColor = white
	}
	if !renderer.button.Dropdown {
		renderer.chevronLeft.StrokeColor = color.Transparent
		renderer.chevronRight.StrokeColor = color.Transparent
	}
	canvas.Refresh(renderer.outer)
	canvas.Refresh(renderer.inner)
	canvas.Refresh(renderer.label)
	canvas.Refresh(renderer.chevronLeft)
	canvas.Refresh(renderer.chevronRight)
}

func (renderer *borderedButtonRenderer) Objects() []fyne.CanvasObject { return renderer.objects }
func (renderer *borderedButtonRenderer) Destroy()                     {}
