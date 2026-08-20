//go:build windows

package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	imagedraw "image/draw"
	"image/png"
	"os"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	xdraw "golang.org/x/image/draw"

	"github.com/super-wow-project/swp-launcher/internal/client"
)

// runOnUI centralizes UI mutations. Fyne 2.5 schedules widget refreshes safely
// through its canvas, while newer Fyne releases expose the equivalent as
// fyne.Do. Keeping this shim avoids coupling the launcher to a newer OpenGL
// toolchain solely for that helper.
func runOnUI(fn func()) {
	fn()
}

//go:embed assets/swp-era-fullwindow.png
var eraPanorama []byte

//go:embed assets/swp-guardian-trimmed.png
var guardianArtwork []byte

//go:embed assets/swp-launcher-icon.png
var launcherIcon []byte

func applyLauncherIcon(application fyne.App) {
	application.SetIcon(fyne.NewStaticResource("swp-launcher-icon.png", launcherIcon))
}

var (
	navy      = color.NRGBA{R: 8, G: 10, B: 15, A: 255}
	navyLight = color.NRGBA{R: 16, G: 18, B: 25, A: 255}
	navyGlass = color.NRGBA{R: 8, G: 10, B: 15, A: 182}
	tabGlass  = color.NRGBA{R: 8, G: 10, B: 15, A: 155}
	gold      = color.NRGBA{R: 212, G: 170, B: 91, A: 255}
	purple    = color.NRGBA{R: 181, G: 105, B: 214, A: 255}
	white     = color.NRGBA{R: 232, G: 228, B: 218, A: 255}
	green     = color.NRGBA{R: 71, G: 210, B: 121, A: 255}
	red       = color.NRGBA{R: 231, G: 69, B: 65, A: 255}
)

type swpTheme struct{ fyne.Theme }

func (swpTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground, theme.ColorNameMenuBackground, theme.ColorNameButton:
		return navy
	case theme.ColorNameInputBackground:
		return white
	case theme.ColorNameOverlayBackground:
		return navy
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameForeground:
		return gold
	case theme.ColorNameForegroundOnPrimary:
		return color.Black
	case theme.ColorNameHover:
		return navyLight
	case theme.ColorNameSelection:
		return color.NRGBA{R: 30, G: 62, B: 89, A: 255}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 190, G: 199, B: 205, A: 255}
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 73, G: 87, B: 98, A: 255}
	}
	return theme.DefaultTheme().Color(name, variant)
}

type fixedHeightLayout struct{ height float32 }

func (l fixedHeightLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, object := range objects {
		object.Resize(fyne.NewSize(size.Width, l.height))
		object.Move(fyne.NewPos(0, 0))
	}
}
func (l fixedHeightLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(1, l.height) }

func fixedHeight(height float32, object fyne.CanvasObject) *fyne.Container {
	return container.New(fixedHeightLayout{height: height}, object)
}

// flushTopLayout deliberately has no theme padding. Fyne's Border layout
// inserts a small gap between its top and centre objects, which exposed the
// artwork as a coloured strip below the navigation bar.
type flushTopLayout struct{ topHeight float32 }

func (layout flushTopLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 2 {
		return
	}
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(size.Width, layout.topHeight))
	objects[1].Move(fyne.NewPos(0, layout.topHeight))
	objects[1].Resize(fyne.NewSize(size.Width, max(size.Height-layout.topHeight, 0)))
}

func (layout flushTopLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) != 2 {
		return fyne.NewSize(1, layout.topHeight)
	}
	contentMinimum := objects[1].MinSize()
	return fyne.NewSize(max(objects[0].MinSize().Width, contentMinimum.Width), layout.topHeight+contentMinimum.Height)
}

type tightMenuLayout struct {
	itemHeight float32
	minWidth   float32
}

type flushColumnsLayout struct{}

func (flushColumnsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	columnWidth := size.Width / float32(len(objects))
	for index, object := range objects {
		x := float32(index) * columnWidth
		width := columnWidth
		if index == len(objects)-1 {
			width = size.Width - x
		}
		object.Move(fyne.NewPos(x, 0))
		object.Resize(fyne.NewSize(width, size.Height))
	}
}

func (flushColumnsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	width, height := float32(0), float32(0)
	for _, object := range objects {
		minimum := object.MinSize()
		width += minimum.Width
		height = max(height, minimum.Height)
	}
	return fyne.NewSize(width, height)
}

type menuOverlayLayout struct {
	menuPosition fyne.Position
	menuSize     fyne.Size
}

func (layout menuOverlayLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 2 {
		return
	}
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
	objects[1].Move(layout.menuPosition)
	objects[1].Resize(layout.menuSize)
}

func (layout menuOverlayLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(1, 1) }

type menuDismissLayer struct {
	widget.BaseWidget
	dismiss func()
}

func newMenuDismissLayer(dismiss func()) *menuDismissLayer {
	layer := &menuDismissLayer{dismiss: dismiss}
	layer.ExtendBaseWidget(layer)
	return layer
}

func (layer *menuDismissLayer) CreateRenderer() fyne.WidgetRenderer {
	background := canvas.NewRectangle(color.Transparent)
	return widget.NewSimpleRenderer(background)
}

func (layer *menuDismissLayer) Tapped(*fyne.PointEvent) {
	if layer.dismiss != nil {
		layer.dismiss()
	}
}

func (layout tightMenuLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for index, object := range objects {
		object.Move(fyne.NewPos(0, float32(index)*layout.itemHeight))
		object.Resize(fyne.NewSize(size.Width, layout.itemHeight))
	}
}

func (layout tightMenuLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(layout.minWidth, float32(len(objects))*layout.itemHeight)
}

type leftInsetLayout struct{ inset float32 }

func (layout leftInsetLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, object := range objects {
		object.Move(fyne.NewPos(layout.inset, 0))
		object.Resize(fyne.NewSize(max(size.Width-layout.inset, 0), size.Height))
	}
}

func (layout leftInsetLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(layout.inset, 0)
	}
	minimum := objects[0].MinSize()
	return fyne.NewSize(minimum.Width+layout.inset, minimum.Height)
}

func leftInset(inset float32, object fyne.CanvasObject) *fyne.Container {
	return container.New(leftInsetLayout{inset: inset}, object)
}

type rightInsetLayout struct{ inset float32 }

func (layout rightInsetLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, object := range objects {
		object.Move(fyne.NewPos(0, 0))
		object.Resize(fyne.NewSize(max(size.Width-layout.inset, 0), size.Height))
	}
}

func (layout rightInsetLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(layout.inset, 0)
	}
	minimum := objects[0].MinSize()
	return fyne.NewSize(minimum.Width+layout.inset, minimum.Height)
}

func rightInset(inset float32, object fyne.CanvasObject) *fyne.Container {
	return container.New(rightInsetLayout{inset: inset}, object)
}

type clientSelectorLayout struct{ height float32 }

func (l clientSelectorLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 3 {
		return
	}
	inputWidth := size.Width * 0.5
	if inputWidth < 300 {
		inputWidth = 300
	}
	// Keep the primary client control visually centred in the Play workspace.
	y := (size.Height - l.height) * 0.5
	iconSize := float32(28)
	gap := float32(10)
	buttonWidth := objects[2].MinSize().Width + 20
	groupWidth := iconSize + gap + inputWidth + gap + buttonWidth
	groupX := (size.Width - groupWidth) / 2
	inputX := groupX + iconSize + gap
	objects[0].Move(fyne.NewPos(groupX, y+(l.height-iconSize)/2))
	objects[0].Resize(fyne.NewSize(iconSize, iconSize))
	objects[1].Move(fyne.NewPos(inputX, y))
	objects[1].Resize(fyne.NewSize(inputWidth, l.height))
	objects[2].Move(fyne.NewPos(inputX+inputWidth+gap, y))
	objects[2].Resize(fyne.NewSize(buttonWidth, l.height))
}
func (l clientSelectorLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(720, l.height)
}

func panelBackground(background color.Color, content fyne.CanvasObject) *fyne.Container {
	return container.NewStack(canvas.NewRectangle(background), content)
}

// framedPanel gives the flat Fyne surfaces the fine, square metallic edge used
// throughout the launcher artwork.
func framedPanel(background color.Color, content fyne.CanvasObject) *fyne.Container {
	surface := canvas.NewRectangle(background)
	surface.StrokeColor = color.NRGBA{R: 91, G: 76, B: 60, A: 210}
	surface.StrokeWidth = 1
	return container.NewStack(surface, container.NewPadded(content))
}

func goldFramedPanel(background color.Color, content fyne.CanvasObject) *fyne.Container {
	surface := canvas.NewRectangle(background)
	surface.StrokeColor = gold
	surface.StrokeWidth = 2
	return container.NewStack(surface, container.NewPadded(content))
}

type launcherBodyLayout struct {
	leftWidth, rightWidth, gap float32
}

type foregroundArtworkLayout struct{ width float32 }

func (layout foregroundArtworkLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, object := range objects {
		object.Move(fyne.NewPos((size.Width-layout.width)/2, 0))
		object.Resize(fyne.NewSize(layout.width, size.Height))
	}
}

func (layout foregroundArtworkLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(1, 1)
}

type statueOverlayLayout struct{}

func (statueOverlayLayout) Layout(objects []fyne.CanvasObject, _ fyne.Size) {
	for _, object := range objects {
		// The clipping raster preserves the visual transform of the original
		// 346x690 image at (-140, 101), but exposes only its in-window pixels.
		object.Move(fyne.NewPos(0, 101))
		object.Resize(fyne.NewSize(206, 650))
	}
}

func (statueOverlayLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(1, 1) }

func clippedGuardianImage(source []byte) image.Image {
	decoded, err := png.Decode(bytes.NewReader(source))
	if err != nil {
		return image.NewNRGBA(image.Rect(0, 0, 206, 650))
	}

	// First reproduce the exact old 346x690 render, then crop the 140 pixels
	// that sat left of the window and the lower part hidden by the footer.
	scaled := image.NewNRGBA(image.Rect(0, 0, 346, 690))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), decoded, decoded.Bounds(), xdraw.Over, nil)
	clipped := image.NewNRGBA(image.Rect(0, 0, 206, 650))
	imagedraw.Draw(clipped, clipped.Bounds(), scaled, image.Pt(140, 0), imagedraw.Src)
	return clipped
}

func (layout launcherBodyLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 3 {
		return
	}
	centerWidth := max(size.Width-layout.leftWidth-layout.rightWidth-layout.gap*2, 1)
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(layout.leftWidth, size.Height))
	objects[1].Move(fyne.NewPos(layout.leftWidth+layout.gap, 0))
	objects[1].Resize(fyne.NewSize(centerWidth, size.Height))
	objects[2].Move(fyne.NewPos(layout.leftWidth+layout.gap+centerWidth+layout.gap, 0))
	objects[2].Resize(fyne.NewSize(layout.rightWidth, size.Height))
}

func (layout launcherBodyLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	height := float32(1)
	for _, object := range objects {
		height = max(height, object.MinSize().Height)
	}
	return fyne.NewSize(layout.leftWidth+layout.rightWidth+layout.gap*2+480, height)
}

func textLabel(text string, size float32, colour color.Color, bold bool) *canvas.Text {
	label := canvas.NewText(text, colour)
	label.TextSize = size
	label.TextStyle.Bold = bold
	return label
}

type stepUI struct {
	icon   *widget.Icon
	detail *canvas.Text
	retry  *hoverIconButton
	root   *fyne.Container
	peers  []stepUI
}

type stepLayout struct{}

func (stepLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 4 {
		return
	}
	icon, heading, detail, retry := objects[0], objects[1], objects[2], objects[3]
	iconSize := float32(24)
	icon.Move(fyne.NewPos(10, 14))
	icon.Resize(fyne.NewSize(iconSize, iconSize))
	heading.Move(fyne.NewPos(44, 8))
	heading.Resize(fyne.NewSize(size.Width-82, 20))
	detailSize := detail.MinSize()
	retrySize := float32(28)
	detail.Move(fyne.NewPos(44, 35))
	detail.Resize(fyne.NewSize(size.Width-82, detailSize.Height))
	retry.Move(fyne.NewPos(size.Width-retrySize-8, 28))
	retry.Resize(fyne.NewSize(retrySize, retrySize))
}

func (stepLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(240, 66) }

var (
	statusPending = fyne.NewStaticResource("status-pending.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="28" height="28" viewBox="0 0 28 28"><path d="M7 7l14 14M21 7L7 21" stroke="#e4b148" stroke-width="3.2" stroke-linecap="round"/></svg>`))
	statusWorking = fyne.NewStaticResource("status-working.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="28" height="28" viewBox="0 0 28 28"><path d="M5 14h18" stroke="#e4b148" stroke-width="3.2" stroke-linecap="round" stroke-dasharray="3 5"/></svg>`))
	statusSuccess = fyne.NewStaticResource("status-success.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="28" height="28" viewBox="0 0 28 28"><path d="M5 14l6 6L23 7" fill="none" stroke="#22ae60" stroke-width="3.2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
	statusFailed  = fyne.NewStaticResource("status-failed.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="28" height="28" viewBox="0 0 28 28"><path d="M7 7l14 14M21 7L7 21" stroke="#e74541" stroke-width="3.2" stroke-linecap="round"/></svg>`))
)

func newStep(number, title, detail string, retry func()) (*fyne.Container, stepUI) {
	icon := widget.NewIcon(statusPending)
	heading := textLabel(number+"  "+title, 14, gold, true)
	heading.Alignment = fyne.TextAlignLeading
	status := textLabel(detail, 12, gold, true)
	status.Alignment = fyne.TextAlignLeading
	retryButton := newHoverIconButton(retry)
	content := container.New(stepLayout{}, icon, heading, status, retryButton)
	return content, stepUI{icon: icon, detail: status, retry: retryButton, root: content}
}

func (step stepUI) pending(message string) {
	step.update(statusPending, message, gold)
}
func (step stepUI) working(message string) {
	step.update(statusWorking, message, gold)
}
func (step stepUI) complete(message string) {
	step.update(statusSuccess, message, green)
}
func (step stepUI) failed(message string) {
	step.update(statusFailed, message, red)
}

func (step stepUI) update(resource fyne.Resource, message string, colour color.Color) {
	steps := append([]stepUI{step}, step.peers...)
	for _, target := range steps {
		target.icon.SetResource(resource)
		target.detail.Text, target.detail.Color = message, colour
		target.detail.Refresh()
		target.root.Refresh()
	}
}

func (step stepUI) setRetryEnabled(enabled bool) {
	steps := append([]stepUI{step}, step.peers...)
	for _, target := range steps {
		if enabled {
			target.retry.Enable()
		} else {
			target.retry.Disable()
		}
	}
}

func runSelfUpdateWindow(args []string) {
	a := app.NewWithID("org.superwowproject.launcher.updater")
	applyLauncherIcon(a)
	a.Settings().SetTheme(swpTheme{theme.DefaultTheme()})
	w := a.NewWindow("Updating SWP Launcher")
	title := textLabel("SUPER WOW PROJECT", 25, gold, true)
	title.Alignment = fyne.TextAlignCenter
	subtitle := textLabel("AUTOMATIC LAUNCHER UPDATE", 18, white, true)
	subtitle.Alignment = fyne.TextAlignCenter
	status := textLabel("Preparing the latest launcher…", 16, gold, true)
	status.Alignment = fyne.TextAlignCenter
	hint := textLabel("The launcher will restart automatically.", 13, white, false)
	hint.Alignment = fyne.TextAlignCenter
	activity := widget.NewActivity()
	activity.Start()
	body := panelBackground(navy, container.NewCenter(container.NewVBox(
		container.NewCenter(title),
		container.NewCenter(subtitle),
		container.NewCenter(status),
		container.NewCenter(activity),
		container.NewCenter(hint),
	)))
	w.SetContent(body)
	w.Resize(fyne.NewSize(1180, 760))
	go func() {
		client.RunSelfUpdateHelperWithProgress(args, func(message string) {
			runOnUI(func() { status.Text = message; status.Refresh() })
		})
		time.Sleep(time.Second)
		runOnUI(w.Close)
	}()
	w.ShowAndRun()
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--swp-apply-update" {
		runSelfUpdateWindow(os.Args[1:])
		return
	}

	a := app.NewWithID("org.superwowproject.launcher")
	applyLauncherIcon(a)
	a.Settings().SetTheme(swpTheme{theme.DefaultTheme()})
	w := a.NewWindow("SWP Launcher " + client.LauncherVersion)
	w.Resize(fyne.NewSize(1180, 760))
	w.SetFixedSize(true)

	current := loadSettings()
	realmEnvironments := []client.RealmEnvironment{
		{ID: "ptr", Name: "PTR", Realmlist: "163.172.51.144", RealmName: "PTR"},
		{ID: "production", Name: "Production", Realmlist: "163.172.51.144", RealmName: "Production"},
	}
	if current.Environment == "" {
		current.Environment = "ptr"
	}
	var environmentButtons []*borderedButton
	var clearEnvironmentError func()
	selectedEnvironment := func() (client.RealmEnvironment, bool) {
		for _, environment := range realmEnvironments {
			if environment.ID == current.Environment {
				return environment, true
			}
		}
		return client.RealmEnvironment{}, false
	}
	syncEnvironmentButtons := func() {
		selected, found := selectedEnvironment()
		if !found && len(realmEnvironments) > 0 {
			current.Environment = realmEnvironments[0].ID
			selected = realmEnvironments[0]
			saveSettings(current)
		}
		for _, button := range environmentButtons {
			button.Text = selected.Name
			button.Refresh()
		}
	}
	newEnvironmentButton := func() *borderedButton {
		var button *borderedButton
		var menuOverlay fyne.CanvasObject
		hideMenu := func() {
			if menuOverlay != nil {
				w.Canvas().Overlays().Remove(menuOverlay)
				menuOverlay = nil
			}
			if button != nil {
				button.DropdownOpen = false
				button.Refresh()
			}
		}
		button = newBorderedButton("SERVER", func() {
			if menuOverlay != nil {
				hideMenu()
				return
			}
			options := make([]fyne.CanvasObject, 0, len(realmEnvironments))
			for _, configuredEnvironment := range realmEnvironments {
				environment := configuredEnvironment
				option := newBorderedButton(environment.Name, func() {
					current.Environment = environment.ID
					saveSettings(current)
					syncEnvironmentButtons()
					hideMenu()
					if clearEnvironmentError != nil {
						clearEnvironmentError()
					}
				})
				options = append(options, option)
			}
			menuContent := container.New(tightMenuLayout{itemHeight: 42, minWidth: button.Size().Width}, options...)
			buttonPosition := fyne.CurrentApp().Driver().AbsolutePositionForObject(button)
			menuSize := fyne.NewSize(button.Size().Width, float32(len(options))*42)
			dismissLayer := newMenuDismissLayer(hideMenu)
			menuOverlay = container.New(menuOverlayLayout{
				menuPosition: fyne.NewPos(buttonPosition.X, buttonPosition.Y-menuSize.Height),
				menuSize:     menuSize,
			}, dismissLayer, menuContent)
			menuOverlay.Resize(w.Canvas().Size())
			w.Canvas().Overlays().Add(menuOverlay)
			button.DropdownOpen = true
			button.Refresh()
		})
		button.Dropdown = true
		environmentButtons = append(environmentButtons, button)
		return button
	}
	setRealmEnvironments := func(environments []client.RealmEnvironment, defaultEnvironment string) {
		if len(environments) == 0 {
			return
		}
		realmEnvironments = append([]client.RealmEnvironment(nil), environments...)
		if _, found := selectedEnvironment(); !found {
			current.Environment = defaultEnvironment
		}
		syncEnvironmentButtons()
	}
	clientValidated, contentChecked, contentReady, busy := false, false, false, false
	pathText := textLabel(current.ClientPath, 13, color.Black, false)
	pathText.Alignment = fyne.TextAlignLeading
	pathDisplay := panelBackground(white, container.NewPadded(pathText))
	folderStatus := widget.NewIcon(statusFailed)
	globalStatus := textLabel("Starting launcher…", 13, white, true)
	clearEnvironmentError = func() {
		if !strings.HasPrefix(strings.ToLower(globalStatus.Text), "launch error:") {
			return
		}
		if contentReady {
			globalStatus.Text = "Client ready"
		} else {
			globalStatus.Text = "Server changed. Complete verification before playing."
		}
		globalStatus.Refresh()
	}
	downloadProgress := widget.NewProgressBar()
	downloadProgress.Min, downloadProgress.Max = 0, 1
	downloadProgress.Hide()

	var validateStep, checkStep, patchStep stepUI
	var selectButton, playButton *borderedButton
	var launcherUpdateButton *hoverIconButton

	refreshButtons := func() {
		if busy {
			selectButton.Disable()
		} else {
			selectButton.Enable()
		}
		if busy || strings.TrimSpace(pathText.Text) == "" {
			validateStep.setRetryEnabled(false)
		} else {
			validateStep.setRetryEnabled(true)
		}
		if busy || !clientValidated {
			checkStep.setRetryEnabled(false)
		} else {
			checkStep.setRetryEnabled(true)
		}
		if busy || !clientValidated || !contentChecked || contentReady {
			patchStep.setRetryEnabled(false)
		} else {
			patchStep.setRetryEnabled(true)
		}
		if busy || strings.TrimSpace(pathText.Text) == "" {
			playButton.Disable()
		} else {
			playButton.Enable()
		}
		if busy {
			launcherUpdateButton.Disable()
		} else {
			launcherUpdateButton.Enable()
		}
	}
	setBusy := func(value bool, message string) {
		busy = value
		if !value {
			downloadProgress.Hide()
			downloadProgress.SetValue(0)
		}
		if message != "" {
			globalStatus.Text = message
			globalStatus.Refresh()
		}
		refreshButtons()
	}
	showDownloadProgress := func(progress client.UpdateProgress) {
		if progress.TotalBytes > 0 {
			downloadProgress.SetValue(float64(progress.BytesDownloaded) / float64(progress.TotalBytes))
			downloadProgress.Show()
		}
		globalStatus.Text = progress.Message
		globalStatus.Refresh()
	}

	var runValidate, runCheck, runPatch, runLauncherUpdate func()
	runValidate = func() {
		folderStatus.SetResource(statusWorking)
		validateStep.working("Reading Wow.exe…")
		value, err := client.Validate(pathText.Text)
		if err != nil {
			folderStatus.SetResource(statusFailed)
			clientValidated, contentChecked, contentReady = false, false, false
			validateStep.failed("Invalid client")
			globalStatus.Text = err.Error()
			globalStatus.Refresh()
			refreshButtons()
			return
		}
		current.ClientPath = value.Path
		saveSettings(current)
		folderStatus.SetResource(statusSuccess)
		clientValidated, contentChecked, contentReady = true, false, false
		validateStep.complete("Build " + value.Version)
		globalStatus.Text = "Client validated"
		globalStatus.Refresh()
		refreshButtons()
		runCheck()
	}
	runCheck = func() {
		checkStep.working("Checking…")
		setBusy(true, "Comparing signed SWP files…")
		root := current.ClientPath
		go func() {
			result, err := client.CheckContent(root)
			runOnUI(func() {
				setBusy(false, "")
				if err != nil {
					contentChecked, contentReady = false, false
					checkStep.failed("Check failed")
					globalStatus.Text = err.Error()
					globalStatus.Refresh()
					return
				}
				contentChecked = true
				if result.Current {
					contentReady = true
					checkStep.complete("Up to date")
					patchStep.complete("No patch needed")
					globalStatus.Text = "Client ready"
				} else {
					contentReady = false
					checkStep.complete(fmt.Sprintf("%d file(s) found", result.ChangedFiles))
					patchStep.pending("Patch available")
					globalStatus.Text = "Installing required files…"
				}
				globalStatus.Refresh()
				refreshButtons()
				if !result.Current {
					runPatch()
				}
			})
		}()
	}
	runPatch = func() {
		patchStep.working("Installing…")
		setBusy(true, "Installing signed SWP files…")
		root := current.ClientPath
		go func() {
			message, err := client.UpdateWithDetailedProgress(root, func(progress client.UpdateProgress) {
				runOnUI(func() { patchStep.working(progress.Message); showDownloadProgress(progress) })
			})
			runOnUI(func() {
				setBusy(false, "")
				if err != nil {
					contentReady = false
					patchStep.failed("Patch failed")
					if strings.Contains(strings.ToLower(err.Error()), "rename ") {
						globalStatus.Text = "Error: you need to close your game before updating it."
					} else {
						globalStatus.Text = err.Error()
					}
					globalStatus.Refresh()
					return
				}
				contentReady = true
				patchStep.complete("Installed")
				globalStatus.Text = message
				globalStatus.Refresh()
				refreshButtons()
			})
		}()
	}
	runLauncherUpdate = func() {
		setBusy(true, "Checking for launcher updates…")
		go func() {
			manifest, err := client.FetchManifest()
			if err != nil {
				runOnUI(func() { setBusy(false, "Launcher update check failed: "+err.Error()) })
				return
			}
			if !client.LauncherUpdateAvailable(manifest) {
				runOnUI(func() { setBusy(false, "Launcher is up to date") })
				return
			}
			started, updateErr := client.StartLauncherUpdateWithProgress(manifest, func(progress string) {
				runOnUI(func() {
					globalStatus.Text = progress
					globalStatus.Refresh()
				})
			})
			if started && updateErr == nil {
				time.Sleep(500 * time.Millisecond)
			}
			runOnUI(func() {
				if updateErr != nil {
					setBusy(false, "Launcher update error: "+updateErr.Error())
				} else if started {
					w.Close()
				} else {
					setBusy(false, "Launcher is up to date")
				}
			})
		}()
	}

	validateContent, validateStepValue := newStep("1", "VALIDATE CLIENT", "Verify game build", func() { runValidate() })
	validateStep = validateStepValue
	checkContent, checkStepValue := newStep("2", "CHECK UPDATES", "Compare signed files", func() { runCheck() })
	checkStep = checkStepValue
	patchContent, patchStepValue := newStep("3", "INSTALL PATCH", "Install required files", func() { runPatch() })
	patchStep = patchStepValue
	newsValidateContent, newsValidateStep := newStep("1", "VALIDATE CLIENT", "Verify game build", func() { runValidate() })
	newsCheckContent, newsCheckStep := newStep("2", "CHECK UPDATES", "Compare signed files", func() { runCheck() })
	newsPatchContent, newsPatchStep := newStep("3", "INSTALL PATCH", "Install required files", func() { runPatch() })
	validateStep.peers = []stepUI{newsValidateStep}
	checkStep.peers = []stepUI{newsCheckStep}
	patchStep.peers = []stepUI{newsPatchStep}

	launcherUpdateButton = newHoverIconButton(func() { runLauncherUpdate() })
	versionRow := container.NewCenter(container.NewHBox(textLabel("VERSION "+client.LauncherVersion, 11, white, false), launcherUpdateButton))
	projectTitle := textLabel("SUPER WOW PROJECT", 27, white, true)
	projectTitle.Alignment = fyne.TextAlignCenter
	projectSubtitle := textLabel("VANILLA  •  THE BURNING CRUSADE  •  WRATH OF THE LICH KING", 11, gold, true)
	projectSubtitle.Alignment = fyne.TextAlignCenter
	headerTitle := container.NewVBox(container.NewCenter(projectTitle), container.NewCenter(projectSubtitle))
	header := fixedHeight(96, container.NewCenter(headerTitle))

	selectButton = newBorderedButton("SELECT CLIENT", func() {
		folderDialog := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			path := uri.Path()
			pathText.Text = path
			pathText.Refresh()
			current.ClientPath = path
			saveSettings(current)
			folderStatus.SetResource(statusWorking)
			clientValidated, contentChecked, contentReady = false, false, false
			validateStep.pending("Verify game build")
			checkStep.pending("Compare signed files")
			patchStep.pending("Install required files")
			refreshButtons()
			runValidate()
		}, w)
		folderDialog.Resize(fyne.NewSize(920, 680))
		folderDialog.Show()
	})
	selector := container.New(clientSelectorLayout{height: 42}, folderStatus, pathDisplay, selectButton)
	background := canvas.NewImageFromResource(fyne.NewStaticResource("swp-era.png", eraPanorama))
	background.FillMode = canvas.ImageFillStretch
	playSurface := container.NewStack(selector)

	validation := container.NewVBox(
		framedPanel(navyLight, validateContent),
		widget.NewSeparator(),
		framedPanel(navyLight, checkContent),
		widget.NewSeparator(),
		framedPanel(navyLight, patchContent),
	)
	configureSelectedRealm := func() error {
		environment, found := selectedEnvironment()
		if !found {
			return fmt.Errorf("select a server environment")
		}
		if err := client.ConfigureRealm(current.ClientPath, environment.Realmlist, environment.RealmName); err != nil {
			return fmt.Errorf("configure %s: %w", environment.Name, err)
		}
		return nil
	}
	showLaunchError := func(err error) {
		message := err.Error()
		if strings.HasPrefix(strings.ToLower(message), "configure ") {
			message += ". Close the game and check that the client folder is writable."
		}
		globalStatus.Text = "Launch error: " + message
		globalStatus.Refresh()
	}
	launchAccount := func(profileID string) error {
		if err := configureSelectedRealm(); err != nil {
			return err
		}
		for _, profile := range current.Accounts {
			if profile.ID != profileID {
				continue
			}
			username, password, err := client.ReadAccountCredential(profile.ID)
			if err != nil {
				return fmt.Errorf("cannot read the saved Windows credential: %w", err)
			}
			w.Clipboard().SetContent(password)
			return client.LaunchWithAccount(current.ClientPath, username)
		}
		return fmt.Errorf("select a saved account")
	}
	checkForUpdatesThenLaunch := func(launch func() error, progress func(string), complete func(), failed func(error)) {
		if busy {
			failed(fmt.Errorf("please wait for the current launcher task to finish"))
			return
		}
		setBusy(true, "Checking for game updates before launch…")
		root := current.ClientPath
		go func() {
			status, err := client.CheckContent(root)
			checkFailed := err != nil
			updatePending := err == nil && !status.Current
			updateCompleted := false
			launchAttempted := false
			if updatePending {
				runOnUI(func() {
					checkStep.complete(fmt.Sprintf("%d file(s) found", status.ChangedFiles))
					patchStep.working("Installing…")
				})
				_, err = client.UpdateWithDetailedProgress(root, func(update client.UpdateProgress) {
					runOnUI(func() {
						patchStep.working(update.Message)
						showDownloadProgress(update)
						progress(update.Message)
					})
				})
				if err == nil {
					var verified client.ContentStatus
					verified, err = client.CheckContent(root)
					if err == nil && !verified.Current {
						err = fmt.Errorf("update verification failed: %d managed file(s) are still outdated", verified.ChangedFiles)
					}
				}
				updateCompleted = err == nil
			}
			if err == nil {
				launchAttempted = true
				err = launch()
			}
			runOnUI(func() {
				setBusy(false, "")
				if err != nil {
					contentChecked, contentReady = launchAttempted, launchAttempted
					if checkFailed {
						checkStep.failed("Check failed")
					} else if updatePending && !updateCompleted {
						patchStep.failed("Update failed")
					} else {
						checkStep.complete("Up to date")
						if updateCompleted {
							patchStep.complete("Installed")
						} else {
							patchStep.complete("No patch needed")
						}
					}
					failed(err)
					refreshButtons()
					return
				}
				clientValidated, contentChecked, contentReady = true, true, true
				folderStatus.SetResource(statusSuccess)
				checkStep.complete("Up to date")
				if updateCompleted {
					patchStep.complete("Installed")
				} else {
					patchStep.complete("No patch needed")
				}
				globalStatus.Text = "Game launched"
				globalStatus.Refresh()
				refreshButtons()
				complete()
			})
		}()
	}
	launchGame := func() {
		checkForUpdatesThenLaunch(func() error {
			if current.DefaultAccount != "" {
				return launchAccount(current.DefaultAccount)
			}
			if err := configureSelectedRealm(); err != nil {
				return err
			}
			return client.Launch(current.ClientPath)
		}, func(string) {}, func() {}, showLaunchError)
	}
	playButton = newBorderedButton("PLAY", launchGame)
	playButton.Primary = true
	playEnvironmentButton := newEnvironmentButton()
	progressDisplay := container.NewGridWrap(fyne.NewSize(220, 18), downloadProgress)
	footerStatus := leftInset(20, container.NewHBox(globalStatus, container.NewCenter(progressDisplay)))
	playControls := container.NewHBox(
		versionRow,
		container.NewGridWrap(fyne.NewSize(150, 42), playEnvironmentButton),
		container.NewGridWrap(fyne.NewSize(120, 42), playButton),
	)
	footerControls := rightInset(20, container.NewCenter(playControls))
	footer := fixedHeight(62, framedPanel(color.NRGBA{R: 8, G: 9, B: 14, A: 218}, container.NewBorder(nil, nil, footerStatus, footerControls)))
	playPage := framedPanel(navyGlass, container.NewPadded(playSurface))

	newsStatus := widget.NewLabel("Loading server news…")
	newsStatus.Wrapping = fyne.TextWrapWord
	newsContent := container.NewVBox()
	newsScroll := container.NewVScroll(newsContent)
	newsScroll.SetMinSize(fyne.NewSize(760, 360))
	var loadNews func()
	loadNews = func() {
		newsStatus.SetText("Loading server news…")
		go func() {
			items, err := client.FetchNews()
			runOnUI(func() {
				newsContent.RemoveAll()
				if err != nil {
					newsStatus.SetText("Unable to load news: " + err.Error())
					return
				}
				if len(items) == 0 {
					newsStatus.SetText("No server news has been published yet.")
					placeholder := widget.NewRichTextFromMarkdown("## News will appear here\n\nServer announcements, maintenance notices, events and content updates will be listed here automatically.")
					placeholder.Wrapping = fyne.TextWrapWord
					newsContent.Add(framedPanel(navyLight, container.NewPadded(placeholder)))
					return
				}
				newsStatus.SetText(fmt.Sprintf("%d server news item(s)", len(items)))
				for _, item := range items {
					date := item.PublishedAt
					if len(date) > 10 {
						date = date[:10]
					}
					title := widget.NewRichTextFromMarkdown("## " + item.Title + "\n\n" + date + "\n\n" + item.Body)
					title.Wrapping = fyne.TextWrapWord
					newsContent.Add(framedPanel(navyLight, container.NewPadded(title)))
				}
			})
		}()
	}
	newsRefresh := newHoverIconButton(loadNews)
	newsHeader := container.NewVBox(
		container.NewBorder(nil, nil, textLabel("LATEST NEWS", 12, gold, true), newsRefresh),
		newsStatus,
	)
	syncEnvironmentButtons()
	_ = newsValidateContent
	_ = newsCheckContent
	_ = newsPatchContent
	newsPage := framedPanel(navyGlass, container.NewPadded(container.NewBorder(newsHeader, nil, nil, nil, newsScroll)))

	accountLabel := widget.NewEntry()
	accountLabel.SetPlaceHolder("Display name, e.g. Main account")
	accountUsername := widget.NewEntry()
	accountUsername.SetPlaceHolder("Account name")
	accountPassword := widget.NewPasswordEntry()
	accountPassword.SetPlaceHolder("Password (stored by Windows)")
	accountMessage := widget.NewLabel("Passwords are encrypted and stored in Windows Credential Manager.")
	accountMessage.Wrapping = fyne.TextWrapWord
	launchAccountAfterUpdateCheck := func(profile accountProfile) {
		accountMessage.SetText("Checking for game updates before launching " + profile.Label + "…")
		checkForUpdatesThenLaunch(func() error {
			return launchAccount(profile.ID)
		}, accountMessage.SetText, func() {
			accountMessage.SetText("Game launched")
		}, func(err error) {
			message := "Launch stopped: " + err.Error()
			accountMessage.SetText(message)
			globalStatus.Text = message
			globalStatus.Refresh()
		})
	}
	selectedAccountID := ""
	accountTiles := container.NewGridWrap(fyne.NewSize(300, 96))
	accountTilesScroll := container.NewVScroll(accountTiles)
	accountTilesScroll.SetMinSize(fyne.NewSize(700, 215))
	editorTitle := widget.NewLabel("ADD ACCOUNT")
	editorTitle.TextStyle.Bold = true
	var accountEditor *fyne.Container
	var accountEditorDialog *dialog.CustomDialog
	showAccountEditor := func() {
		if accountEditorDialog != nil {
			accountEditorDialog.Hide()
		}
		accountEditorDialog = dialog.NewCustomWithoutButtons("ACCOUNT", accountEditor, w)
		accountEditorDialog.Resize(fyne.NewSize(560, 340))
		accountEditorDialog.Show()
	}
	hideAccountEditor := func() {
		if accountEditorDialog != nil {
			accountEditorDialog.Hide()
			accountEditorDialog = nil
		}
	}
	openNewAccount := func() {
		selectedAccountID = ""
		accountLabel.SetText("")
		accountUsername.SetText("")
		accountPassword.SetText("")
		editorTitle.SetText("ADD ACCOUNT")
		accountMessage.SetText("Enter the new account details, then click Save account.")
		showAccountEditor()
	}
	openAccountEditor := func(profile accountProfile) {
		selectedAccountID = profile.ID
		accountLabel.SetText(profile.Label)
		accountUsername.SetText(profile.Username)
		accountPassword.SetText("")
		editorTitle.SetText("EDIT " + strings.ToUpper(profile.Label))
		accountMessage.SetText("Enter the password again only when saving account changes.")
		showAccountEditor()
	}
	var refreshAccounts func()
	saveAccount := widget.NewButtonWithIcon("Save account", theme.DocumentSaveIcon(), func() {
		label := strings.TrimSpace(accountLabel.Text)
		username := strings.TrimSpace(accountUsername.Text)
		if label == "" || username == "" || accountPassword.Text == "" {
			accountMessage.SetText("A display name, account name and password are required.")
			return
		}
		id := selectedAccountID
		if id == "" {
			id = strconv.FormatInt(time.Now().UnixNano(), 36)
			current.Accounts = append(current.Accounts, accountProfile{ID: id})
		}
		if err := client.StoreAccountCredential(id, username, accountPassword.Text); err != nil {
			accountMessage.SetText("Unable to save the Windows credential: " + err.Error())
			return
		}
		for index := range current.Accounts {
			if current.Accounts[index].ID == id {
				current.Accounts[index].Label = label
				current.Accounts[index].Username = username
			}
		}
		saveSettings(current)
		accountPassword.SetText("")
		accountMessage.SetText("Account saved securely.")
		hideAccountEditor()
		refreshAccounts()
	})
	deleteAccount := widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), func() {
		if selectedAccountID == "" {
			return
		}
		_ = client.DeleteAccountCredential(selectedAccountID)
		filtered := current.Accounts[:0]
		for _, profile := range current.Accounts {
			if profile.ID != selectedAccountID {
				filtered = append(filtered, profile)
			}
		}
		current.Accounts = filtered
		if current.DefaultAccount == selectedAccountID {
			current.DefaultAccount = ""
		}
		saveSettings(current)
		accountLabel.SetText("")
		accountUsername.SetText("")
		accountPassword.SetText("")
		accountMessage.SetText("Account removed.")
		hideAccountEditor()
		refreshAccounts()
	})
	cancelAccountEdit := widget.NewButton("Cancel", func() {
		hideAccountEditor()
	})
	accountForm := widget.NewForm(
		widget.NewFormItem("Display name", blackInput(accountLabel)),
		widget.NewFormItem("Account name", blackInput(accountUsername)),
		widget.NewFormItem("Password", blackInput(accountPassword)),
	)
	credentialStorageNote := widget.NewLabel("Passwords remain encrypted in Windows Credential Manager and never appear in launcher settings or command lines.")
	credentialStorageNote.Wrapping = fyne.TextWrapWord
	accountEditor = goldFramedPanel(navyLight, container.NewPadded(container.NewVBox(
		editorTitle,
		accountForm,
		credentialStorageNote,
		container.NewHBox(saveAccount, deleteAccount, cancelAccountEdit),
	)))
	refreshAccounts = func() {
		accountTiles.RemoveAll()
		for _, storedProfile := range current.Accounts {
			profile := storedProfile
			tile := newAccountTile(profile.Label, profile.Username, profile.ID == current.DefaultAccount, func() {
				launchAccountAfterUpdateCheck(profile)
			}, func() {
				current.DefaultAccount = profile.ID
				saveSettings(current)
				accountMessage.SetText(profile.Label + " is now the default account.")
				refreshAccounts()
			}, func() { openAccountEditor(profile) })
			accountTiles.Add(tile)
		}
		accountTiles.Add(newAddAccountTile(openNewAccount))
		accountTiles.Refresh()
	}
	accountsInstruction := widget.NewLabel("Click an account tile to launch the game and copy its password. Use the star to make it the default account.")
	accountsInstruction.Wrapping = fyne.TextWrapWord
	accountsHeader := container.NewVBox(
		textLabel("YOUR ACCOUNTS", 12, gold, true),
		accountMessage,
		accountsInstruction,
	)
	accountsLayout := container.NewBorder(accountsHeader, nil, nil, nil, accountTilesScroll)
	accountsPage := framedPanel(navyGlass, container.NewPadded(accountsLayout))
	refreshAccounts()

	var autoUpdateChecks []*widget.Check
	syncingConfiguration := false
	syncConfiguration := func() {
		if syncingConfiguration {
			return
		}
		syncingConfiguration = true
		defer func() { syncingConfiguration = false }()
		for _, check := range autoUpdateChecks {
			check.SetChecked(current.AutoUpdate)
		}
	}
	newConfigurationControls := func() *widget.Check {
		autoUpdate := widget.NewCheck("Automatically install launcher updates at startup", func(checked bool) {
			if syncingConfiguration {
				return
			}
			current.AutoUpdate = checked
			saveSettings(current)
			syncConfiguration()
		})
		autoUpdateChecks = append(autoUpdateChecks, autoUpdate)
		return autoUpdate
	}
	fullAutoUpdateCheck := newConfigurationControls()
	configurationPrivacyNote := widget.NewLabel("Saved account passwords are copied to the clipboard at launch and are never typed automatically.")
	configurationPrivacyNote.Wrapping = fyne.TextWrapWord
	configurationPage := framedPanel(navyGlass, container.NewPadded(container.NewVBox(
		textLabel("LAUNCHER SETTINGS", 12, gold, true),
		widget.NewLabel("Launcher behaviour"),
		fullAutoUpdateCheck,
		widget.NewSeparator(),
		configurationPrivacyNote,
	)))
	syncConfiguration()

	pages := []fyne.CanvasObject{newsPage, accountsPage, playPage, configurationPage}
	pageHost := container.NewStack(pages[0])
	var tabButtons []*hoverTabButton
	selectTab := func(index int) {
		pageHost.RemoveAll()
		pageHost.Add(pages[index])
		for buttonIndex, button := range tabButtons {
			button.selected = buttonIndex == index
			button.Refresh()
		}
	}
	tabSpecs := []struct {
		text string
		icon []byte
	}{{"News", swpNewsIcon}, {"Accounts", swpAccountsIcon}, {"Client", swpClientIcon}, {"Configuration", swpConfigurationIcon}}
	for index, spec := range tabSpecs {
		tabIndex := index
		tabButtons = append(tabButtons, newHoverTabButton(spec.text, spec.icon, func() { selectTab(tabIndex) }))
	}
	tabButtons[0].selected = true
	tabObjects := make([]fyne.CanvasObject, len(tabButtons))
	for index, button := range tabButtons {
		tabObjects[index] = button
	}
	tabStripFrame := canvas.NewRectangle(color.Transparent)
	tabStripFrame.StrokeColor = color.NRGBA{R: 86, G: 73, B: 58, A: 160}
	tabStripFrame.StrokeWidth = 1
	tabBar := fixedHeight(74, container.NewStack(tabStripFrame, container.New(flushColumnsLayout{}, tabObjects...)))
	tabs := container.New(flushTopLayout{topHeight: 74}, tabBar, pageHost)

	guardian := canvas.NewImageFromImage(clippedGuardianImage(guardianArtwork))
	guardian.FillMode = canvas.ImageFillStretch
	brandRail := container.NewStack(canvas.NewRectangle(color.Transparent))

	statusTitle := textLabel("STATUS", 12, gold, true)
	statusRule := canvas.NewRectangle(color.NRGBA{R: 83, G: 69, B: 59, A: 255})
	statusHeader := container.NewVBox(statusTitle, fixedHeight(1, statusRule))
	statusRail := framedPanel(color.NRGBA{R: 10, G: 12, B: 17, A: 150}, container.NewPadded(container.NewBorder(statusHeader, nil, nil, nil, validation)))

	bodyColumns := container.New(launcherBodyLayout{leftWidth: 270, rightWidth: 320, gap: 8}, brandRail, tabs, statusRail)
	coreContent := container.NewBorder(header, footer, nil, nil, container.NewPadded(bodyColumns))
	statueOverlay := container.New(statueOverlayLayout{}, guardian)
	outerFrame := canvas.NewRectangle(color.Transparent)
	outerFrame.StrokeColor = color.NRGBA{R: 91, G: 76, B: 60, A: 235}
	outerFrame.StrokeWidth = 2
	w.SetContent(container.NewStack(background, statueOverlay, coreContent, outerFrame))
	loadNews()

	refreshButtons()
	go func() {
		manifest, updateErr := client.FetchManifest()
		if updateErr == nil {
			runOnUI(func() { setRealmEnvironments(manifest.Realms, manifest.DefaultEnvironment) })
		}
		if current.AutoUpdate && updateErr == nil && client.LauncherUpdateAvailable(manifest) {
			runOnUI(func() {
				setBusy(true, "Downloading launcher update…")
			})
			started, err := client.StartLauncherUpdateWithProgress(manifest, func(progress string) {
				runOnUI(func() {
					globalStatus.Text = progress
					globalStatus.Refresh()
				})
			})
			if started && err == nil {
				time.Sleep(500 * time.Millisecond)
			}
			runOnUI(func() {
				if err != nil {
					setBusy(false, "Launcher update error: "+err.Error())
				} else if started {
					w.Close()
				}
			})
			if started && err == nil {
				return
			}
		}
		runOnUI(func() {
			setBusy(false, "Ready")
			if current.ClientPath != "" {
				runValidate()
			}
		})
	}()
	w.ShowAndRun()
}
