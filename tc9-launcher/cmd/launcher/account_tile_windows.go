//go:build windows

package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

type accountTile struct {
	widget.BaseWidget
	Label, Username      string
	Default, Add         bool
	OnLaunch, OnDefault  func()
	OnEdit               func()
	hovered, starHovered bool
}

var (
	starOutline = fyne.NewStaticResource("star-outline.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M12 2.7l2.8 5.8 6.4.9-4.6 4.5 1.1 6.3-5.7-3-5.7 3 1.1-6.3-4.6-4.5 6.4-.9z" fill="none" stroke="#e4b148" stroke-width="1.8" stroke-linejoin="round"/></svg>`))
	starFilled  = fyne.NewStaticResource("star-filled.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M12 2.7l2.8 5.8 6.4.9-4.6 4.5 1.1 6.3-5.7-3-5.7 3 1.1-6.3-4.6-4.5 6.4-.9z" fill="#e4b148"/></svg>`))
)

func newAccountTile(label, username string, isDefault bool, launch, makeDefault, edit func()) *accountTile {
	tile := &accountTile{Label: label, Username: username, Default: isDefault, OnLaunch: launch, OnDefault: makeDefault, OnEdit: edit}
	tile.ExtendBaseWidget(tile)
	return tile
}

func newAddAccountTile(add func()) *accountTile {
	tile := &accountTile{Label: "+  ADD ACCOUNT", Add: true, OnLaunch: add}
	tile.ExtendBaseWidget(tile)
	return tile
}

func (tile *accountTile) CreateRenderer() fyne.WidgetRenderer {
	border := canvas.NewRectangle(navyLight)
	border.StrokeColor = gold
	border.StrokeWidth = 1
	inner := canvas.NewRectangle(navyLight)
	title := canvas.NewText(tile.Label, white)
	title.TextSize, title.TextStyle.Bold = 16, true
	if tile.Add {
		title.Color = gold
	}
	username := canvas.NewText(tile.Username, gold)
	username.TextSize = 12
	star := canvas.NewImageFromResource(starOutline)
	star.FillMode = canvas.ImageFillContain
	star.Hide()
	action := canvas.NewText("", gold)
	action.TextSize, action.TextStyle.Bold, action.Alignment = 10, true, fyne.TextAlignTrailing
	editText := ""
	if !tile.Add {
		star.Show()
		editText = "EDIT"
	}
	edit := canvas.NewText(editText, gold)
	edit.TextSize, edit.TextStyle.Bold, edit.Alignment = 10, true, fyne.TextAlignTrailing
	objects := []fyne.CanvasObject{border, inner, title, username, star, action, edit}
	return &accountTileRenderer{tile: tile, border: border, inner: inner, title: title, username: username, star: star, action: action, edit: edit, objects: objects}
}

func (tile *accountTile) Tapped(event *fyne.PointEvent) {
	if tile.Add {
		if tile.OnLaunch != nil {
			tile.OnLaunch()
		}
		return
	}
	if event.Position.X >= tile.Size().Width-155 && event.Position.Y <= 42 {
		if tile.OnDefault != nil {
			tile.OnDefault()
		}
		return
	}
	if event.Position.X >= tile.Size().Width-70 && event.Position.Y > 55 {
		if tile.OnEdit != nil {
			tile.OnEdit()
		}
		return
	}
	if tile.OnLaunch != nil {
		tile.OnLaunch()
	}
}

func (tile *accountTile) MouseIn(event *desktop.MouseEvent) {
	tile.hovered = true
	tile.updateStarHover(event.Position)
}

func (tile *accountTile) MouseMoved(event *desktop.MouseEvent) { tile.updateStarHover(event.Position) }

func (tile *accountTile) updateStarHover(position fyne.Position) {
	wanted := !tile.Add && position.X >= tile.Size().Width-155 && position.Y <= 42
	if tile.starHovered != wanted {
		tile.starHovered = wanted
		tile.Refresh()
	}
}

func (tile *accountTile) MouseOut() {
	tile.hovered, tile.starHovered = false, false
	tile.Refresh()
}

func (tile *accountTile) Cursor() desktop.Cursor { return desktop.PointerCursor }

type accountTileRenderer struct {
	tile                          *accountTile
	border, inner                 *canvas.Rectangle
	title, username, action, edit *canvas.Text
	star                          *canvas.Image
	objects                       []fyne.CanvasObject
}

func (renderer *accountTileRenderer) Layout(size fyne.Size) {
	renderer.border.Resize(size)
	renderer.inner.Move(fyne.NewPos(1, 1))
	renderer.inner.Resize(fyne.NewSize(size.Width-2, size.Height-2))
	renderer.title.Move(fyne.NewPos(16, 17))
	renderer.title.Resize(fyne.NewSize(size.Width-175, 24))
	renderer.username.Move(fyne.NewPos(16, 50))
	renderer.username.Resize(fyne.NewSize(size.Width-100, 20))
	renderer.star.Move(fyne.NewPos(size.Width-154, 17))
	renderer.star.Resize(fyne.NewSize(18, 18))
	renderer.action.Move(fyne.NewPos(size.Width-133, 16))
	renderer.action.Resize(fyne.NewSize(118, 22))
	renderer.edit.Move(fyne.NewPos(size.Width-70, 67))
	renderer.edit.Resize(fyne.NewSize(55, 18))
}

func (renderer *accountTileRenderer) MinSize() fyne.Size { return fyne.NewSize(300, 96) }

func (renderer *accountTileRenderer) Refresh() {
	renderer.title.Text, renderer.username.Text = renderer.tile.Label, renderer.tile.Username
	renderer.border.FillColor = navyLight
	renderer.border.StrokeColor = gold
	if renderer.tile.hovered {
		renderer.inner.FillColor = color.NRGBA{R: 32, G: 61, B: 84, A: 255}
	} else {
		renderer.inner.FillColor = navyLight
	}
	if renderer.tile.Add {
		renderer.action.Text, renderer.edit.Text = "", ""
		renderer.star.Hide()
		renderer.title.Color = gold
	} else {
		renderer.star.Show()
		renderer.edit.Text = "EDIT"
		switch {
		case renderer.tile.Default:
			renderer.star.Resource = starFilled
			renderer.action.Text = "DEFAULT"
		case renderer.tile.starHovered:
			renderer.star.Resource = starOutline
			renderer.action.Text = "SET AS DEFAULT"
		default:
			renderer.star.Resource = starOutline
			renderer.action.Text = "SET DEFAULT"
		}
	}
	for _, object := range renderer.objects {
		canvas.Refresh(object)
	}
}

func (renderer *accountTileRenderer) Objects() []fyne.CanvasObject { return renderer.objects }
func (renderer *accountTileRenderer) Destroy()                     {}
