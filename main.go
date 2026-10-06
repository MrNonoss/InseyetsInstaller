package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"golang.org/x/image/draw"
	"golang.org/x/sys/windows"
)

// Embedded default logos
//
//go:embed assets/dcode_logo.png
var defaultDCodeLogoBytes []byte

//go:embed assets/pa_logo.png
var defaultPALogoBytes []byte

//go:embed assets/ufed_logo.png
var defaultUFEDLogoBytes []byte

// Global UI controls
var (
	mw *walk.MainWindow

	// Header controls
	cwDetectStatus   *walk.CustomWidget
	detectStatusText string
	boldFontObj      *walk.Font

	// 1-DCode Controls
	ivDCodeLogo    *walk.ImageView
	leDCodePath    *walk.LineEdit
	btnDCodeBrowse *walk.PushButton
	btnDCodeClear  *walk.PushButton

	// 2-PA Controls
	ivPALogo            *walk.ImageView
	lePAPath            *walk.LineEdit
	btnPABrowse         *walk.PushButton
	btnPAClear          *walk.PushButton
	cbPACollectLogs     *walk.CheckBox
	cbPASkipValidations *walk.CheckBox
	cbPAInstallCloud    *walk.CheckBox
	cbPAInstallMaps     *walk.CheckBox

	// 3-UFED Controls
	ivUFEDLogo        *walk.ImageView
	leUFEDPath        *walk.LineEdit
	btnUFEDBrowse     *walk.PushButton
	btnUFEDClear      *walk.PushButton
	cbUFEDCollectLogs *walk.CheckBox
	cbUFEDRestart     *walk.CheckBox

	// Action Controls
	btnStart      *walk.PushButton
	btnOpenFolder *walk.PushButton
	btnClearLog   *walk.PushButton
	lblTimer      *walk.Label
	pbProgress    *walk.ProgressBar
	lblStatus     *walk.Label
	teLog         *walk.TextEdit

	// Execution state
	isInstalling  bool
	installMutex  sync.Mutex
	timerStopChan chan struct{}
	toolDir       string

	// Resized exact bitmaps
	dcodeBitmap *walk.Bitmap
	paBitmap    *walk.Bitmap
	ufedBitmap  *walk.Bitmap
)

func getToolDirectory() string {
	exe, err := os.Executable()
	if err == nil {
		return filepath.Dir(exe)
	}
	dir, err := os.Getwd()
	if err == nil {
		return dir
	}
	return "."
}

func centerWindow(hwnd win.HWND) {
	if hwnd == 0 {
		return
	}
	var rect win.RECT
	if !win.GetWindowRect(hwnd, &rect) {
		return
	}
	winWidth := int(rect.Right - rect.Left)
	winHeight := int(rect.Bottom - rect.Top)
	if winWidth <= 0 {
		winWidth = 980
	}
	if winHeight <= 0 {
		winHeight = 720
	}

	hMon := win.MonitorFromWindow(hwnd, win.MONITOR_DEFAULTTONEAREST)
	var mi win.MONITORINFO
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if win.GetMonitorInfo(hMon, &mi) {
		workWidth := int(mi.RcWork.Right - mi.RcWork.Left)
		workHeight := int(mi.RcWork.Bottom - mi.RcWork.Top)
		x := int(mi.RcWork.Left) + (workWidth-winWidth)/2
		y := int(mi.RcWork.Top) + (workHeight-winHeight)/2
		if x < int(mi.RcWork.Left) {
			x = int(mi.RcWork.Left)
		}
		if y < int(mi.RcWork.Top) {
			y = int(mi.RcWork.Top)
		}
		win.SetWindowPos(hwnd, 0, int32(x), int32(y), 0, 0, win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE)
	} else {
		screenWidth := int(win.GetSystemMetrics(win.SM_CXSCREEN))
		screenHeight := int(win.GetSystemMetrics(win.SM_CYSCREEN))
		x := (screenWidth - winWidth) / 2
		y := (screenHeight - winHeight) / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		win.SetWindowPos(hwnd, 0, int32(x), int32(y), 0, 0, win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE)
	}
}

func bringWindowToFront(hwnd win.HWND) {
	if hwnd == 0 {
		return
	}
	win.ShowWindow(hwnd, win.SW_SHOWNORMAL)
	win.SetWindowPos(hwnd, win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE)
	win.SetWindowPos(hwnd, win.HWND_NOTOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE)
	win.SetForegroundWindow(hwnd)
	win.BringWindowToTop(hwnd)
}

// Loads an image from a custom path (if exists) or embedded fallback, and scales it to exact targetSize x targetSize
func loadExactBitmap(customPath string, embeddedBytes []byte, targetSize int) *walk.Bitmap {
	var srcImg image.Image
	if customPath != "" {
		if f, err := os.Open(customPath); err == nil {
			srcImg, _, _ = image.Decode(f)
			f.Close()
		}
	}
	if srcImg == nil {
		srcImg, _ = png.Decode(bytes.NewReader(embeddedBytes))
	}
	if srcImg == nil {
		return nil
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetSize, targetSize))
	draw.BiLinear.Scale(dst, dst.Bounds(), srcImg, srcImg.Bounds(), draw.Over, nil)

	bmp, err := walk.NewBitmapFromImage(dst)
	if err != nil {
		return nil
	}
	return bmp
}

// Argument builders used by both GUI and unit tests
func constructDCodeArgs(exePath string) []string {
	return []string{"/SP-", "/VERYSILENT", "/SUPPRESSMSGBOXES"}
}

func constructPAArgs(exePath string, collectLogs, skipValidations, installCloud, installMaps bool, currentToolDir string) []string {
	args := []string{"/install", "/silent", "/norestart"}

	if collectLogs {
		paLogsDir := filepath.Join(currentToolDir, "PA_logs")
		_ = os.MkdirAll(paLogsDir, 0755)
		logFile := filepath.Join(paLogsDir, "PA_logs.txt")
		args = append(args, "/log", logFile)
	}

	if skipValidations {
		args = append(args, "SKIP_VALIDATIONS=true")
	} else {
		args = append(args, "SKIP_VALIDATIONS=false")
	}

	if installCloud {
		args = append(args, "INSTALL_CLOUD=true")
	} else {
		args = append(args, "INSTALL_CLOUD=false")
	}

	if installMaps {
		args = append(args, "INSTALL_TILE_SERVER=true")
	} else {
		args = append(args, "INSTALL_TILE_SERVER=false")
	}

	return args
}

func constructUFEDArgs(exePath string, collectLogs, restartWhenDone bool, currentToolDir string) []string {
	args := []string{"/SP-", "/VERYSILENT", "/SUPPRESSMSGBOXES"}

	if collectLogs {
		ufedLogsDir := filepath.Join(currentToolDir, "UFED_logs")
		_ = os.MkdirAll(ufedLogsDir, 0755)
		logFile := filepath.Join(ufedLogsDir, "UFED_logs.txt")
		args = append(args, fmt.Sprintf("/LOG=%s", logFile))
	}

	if !restartWhenDone {
		args = append(args, "/NORESTART")
	}

	return args
}

func formatCommandLine(exe string, args []string) string {
	displayExe := exe
	if displayExe == "" {
		displayExe = "[INSTALLER.exe]"
	}

	var sb strings.Builder
	if strings.Contains(displayExe, " ") {
		sb.WriteString(fmt.Sprintf("\"%s\"", displayExe))
	} else {
		sb.WriteString(displayExe)
	}

	for _, a := range args {
		sb.WriteString(" ")
		if strings.Contains(a, " ") && !strings.HasPrefix(a, "\"") {
			sb.WriteString(fmt.Sprintf("\"%s\"", a))
		} else {
			sb.WriteString(a)
		}
	}

	return sb.String()
}

type DetectedInstallers struct {
	DCodePath string
	PAPath    string
	UFEDPath  string
}

// isAsciiLetter checks if a byte is an ASCII letter
func isAsciiLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// hasDigit checks if a string contains at least one digit
func hasDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}

// hasPAToken checks if "pa" appears as an isolated token or in "inseyetspa" / "cellebritepa"
func hasPAToken(s string) bool {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "physical analyzer") || strings.Contains(lower, "physicalanalyzer") {
		return true
	}
	if strings.Contains(lower, "inseyetspa") || strings.Contains(lower, "inseyets_pa") ||
		strings.Contains(lower, "inseyets-pa") || strings.Contains(lower, "cellebrite_pa") ||
		strings.Contains(lower, "cellebrite-pa") || strings.Contains(lower, "cellebritepa") {
		return true
	}

	n := len(lower)
	for i := 0; i < n-1; i++ {
		if lower[i] == 'p' && lower[i+1] == 'a' {
			beforeOK := (i == 0) || !isAsciiLetter(lower[i-1])
			afterOK := (i+2 == n) || !isAsciiLetter(lower[i+2])
			if beforeOK && afterOK {
				return true
			}
		}
	}
	return false
}

// isDCodeInstaller checks if an executable belongs to DCode, robust to version changes
func isDCodeInstaller(filename string) bool {
	lower := strings.ToLower(filename)
	if !strings.HasSuffix(lower, ".exe") {
		return false
	}
	// Exclude non-installers
	if strings.Contains(lower, "portable") || strings.Contains(lower, "reader") ||
		strings.Contains(lower, "viewer") || strings.Contains(lower, "patch") ||
		strings.Contains(lower, "keygen") || strings.Contains(lower, "crack") {
		return false
	}
	// DCode installer pattern: DCode-x86-EN-5.7.26188.46.exe, DCode-Setup.exe, etc.
	if strings.HasPrefix(lower, "dcode-") || strings.HasPrefix(lower, "dcode_") ||
		strings.Contains(lower, "dcodesetup") || strings.Contains(lower, "dcode_setup") ||
		strings.Contains(lower, "dcode-setup") {
		return true
	}
	if strings.HasPrefix(lower, "dcode") && hasDigit(lower) {
		return true
	}
	return false
}

// matchPAAndUFED matches PA and UFED executables within a specific directory
func matchPAAndUFED(folderPath string, exes []string, has7z, hasBin bool) (paExe, ufedExe string) {
	folderLower := strings.ToLower(filepath.Base(folderPath))
	folderHasBrand := strings.Contains(folderLower, "inseyets") || strings.Contains(folderLower, "cellebrite")
	folderHasPA := hasPAToken(folderLower)
	folderHasUFED := strings.Contains(folderLower, "ufed")

	// ----------------------------------------------------
	// Inseyets.PA installer matching
	// ----------------------------------------------------
	for _, exe := range exes {
		exeLower := strings.ToLower(exe)
		if !strings.HasSuffix(exeLower, ".exe") {
			continue
		}
		// Exclude non-installers (e.g. portable tools, readers, viewers, patchers)
		if strings.Contains(exeLower, "portable") || strings.Contains(exeLower, "reader") ||
			strings.Contains(exeLower, "viewer") || strings.Contains(exeLower, "patch") ||
			strings.Contains(exeLower, "keygen") || strings.Contains(exeLower, "crack") {
			continue
		}

		exeHasBrand := strings.Contains(exeLower, "inseyets") || strings.Contains(exeLower, "cellebrite")
		exeHasPA := hasPAToken(exeLower)

		// Rule 1: Folder has .7z payload archives (standard Inseyets.PA package)
		// The exe must have a PA token AND (brand in exe or brand in folder)
		if has7z && exeHasPA && (exeHasBrand || folderHasBrand) {
			paExe = filepath.Join(folderPath, exe)
			break
		}

		// Rule 2: Standalone or unpacked without .7z
		// Must explicitly have brand ("inseyets" or "cellebrite") AND PA token in the executable itself
		if exeHasBrand && exeHasPA && (strings.Contains(exeLower, "setup") || strings.Contains(exeLower, "install") || hasDigit(exeLower)) {
			paExe = filepath.Join(folderPath, exe)
			break
		}

		// Rule 3: Folder is explicitly an Inseyets PA folder (e.g. "Cellebrite_Inseyets_PA_10.11")
		// and exe has PA token
		if folderHasBrand && folderHasPA && exeHasPA {
			paExe = filepath.Join(folderPath, exe)
			break
		}
	}

	// ----------------------------------------------------
	// Inseyets.UFED installer matching
	// ----------------------------------------------------
	for _, exe := range exes {
		exeLower := strings.ToLower(exe)
		if !strings.HasSuffix(exeLower, ".exe") {
			continue
		}
		// Exclude non-installers (UFEDReader, viewers, dumpers, portables)
		if strings.Contains(exeLower, "reader") || strings.Contains(exeLower, "viewer") ||
			strings.Contains(exeLower, "portable") || strings.Contains(exeLower, "dump") ||
			strings.Contains(exeLower, "extractor") || strings.Contains(exeLower, "patch") ||
			strings.Contains(exeLower, "crack") {
			continue
		}

		if !strings.Contains(exeLower, "ufed") {
			continue
		}

		exeHasBrand := strings.Contains(exeLower, "inseyets") || strings.Contains(exeLower, "cellebrite")
		exeIsSetup := strings.Contains(exeLower, "setup") || strings.Contains(exeLower, "install") || strings.Contains(exeLower, "fat")

		// Rule 1: Folder has .bin payload files (standard Inseyets.UFED package)
		// Exe must contain "ufed" and (be a setup/install or have brand in exe or folder)
		if hasBin && (exeIsSetup || exeHasBrand || folderHasBrand || folderHasUFED) {
			ufedExe = filepath.Join(folderPath, exe)
			break
		}

		// Rule 2: Exe without .bin in folder
		// Must be explicitly a UFED Setup/Install executable with brand (Inseyets or Cellebrite)
		// e.g. "Cellebrite UFED Setup 10.11.exe" or "InseyetsUFED_10.11.1.457.exe"
		if (exeHasBrand && exeIsSetup) || (strings.Contains(exeLower, "inseyets") && strings.Contains(exeLower, "ufed") && hasDigit(exeLower)) {
			ufedExe = filepath.Join(folderPath, exe)
			break
		}

		// Rule 3: Folder is explicitly an Inseyets UFED folder (e.g. "InseyetsUFED_10.11.1")
		// and exe is a setup or contains ufed with version digits
		if (folderHasBrand && folderHasUFED) && (exeIsSetup || hasDigit(exeLower)) {
			ufedExe = filepath.Join(folderPath, exe)
			break
		}
	}

	return paExe, ufedExe
}

// compareNatural performs natural sorting (e.g. "5.10" > "5.7")
func compareNatural(a, b string) int {
	i, j := 0, 0
	lenA, lenB := len(a), len(b)

	for i < lenA && j < lenB {
		charA, charB := a[i], b[j]

		if charA >= '0' && charA <= '9' && charB >= '0' && charB <= '9' {
			startI := i
			for i < lenA && a[i] >= '0' && a[i] <= '9' {
				i++
			}
			startJ := j
			for j < lenB && b[j] >= '0' && b[j] <= '9' {
				j++
			}

			chunkA := strings.TrimLeft(a[startI:i], "0")
			chunkB := strings.TrimLeft(b[startJ:j], "0")

			if len(chunkA) != len(chunkB) {
				if len(chunkA) < len(chunkB) {
					return -1
				}
				return 1
			}
			if chunkA != chunkB {
				if chunkA < chunkB {
					return -1
				}
				return 1
			}
			if (i - startI) != (j - startJ) {
				if (i - startI) < (j - startJ) {
					return -1
				}
				return 1
			}
			continue
		}

		lowerA := charA
		if lowerA >= 'A' && lowerA <= 'Z' {
			lowerA += 'a' - 'A'
		}
		lowerB := charB
		if lowerB >= 'A' && lowerB <= 'Z' {
			lowerB += 'a' - 'A'
		}

		if lowerA != lowerB {
			if lowerA < lowerB {
				return -1
			}
			return 1
		}
		i++
		j++
	}

	if i < lenA {
		return 1
	}
	if j < lenB {
		return -1
	}
	return 0
}

func sortCandidates(candidates []string) {
	sort.Slice(candidates, func(i, j int) bool {
		baseI := filepath.Base(candidates[i])
		baseJ := filepath.Base(candidates[j])
		cmp := compareNatural(baseI, baseJ)
		if cmp != 0 {
			return cmp < 0
		}
		return compareNatural(candidates[i], candidates[j]) < 0
	})
}

func isSkippedFolder(nameLower string) bool {
	switch nameLower {
	case "pa_logs", "ufed_logs", "dcode_logs", "assets", "winres", ".git",
		"windows", "program files", "program files (x86)", "programdata",
		"appdata", "application data", "local settings",
		"$recycle.bin", "system volume information",
		"node_modules", ".cache", ".vscode", ".idea", ".gemini":
		return true
	default:
		return false
	}
}

func isSystemRoot(dir string) bool {
	clean := strings.ToLower(filepath.Clean(dir))
	if clean == "c:\\" || clean == "c:" || clean == "/" {
		return true
	}
	if strings.HasPrefix(clean, "c:\\windows") ||
		strings.HasPrefix(clean, "c:\\program files") ||
		strings.HasPrefix(clean, "c:\\programdata") {
		return true
	}
	return false
}

// scanDefaultFolders scans baseDir and its parent directory: up to depth 4 for PA/UFED, and up to depth 7 for DCode
func scanDefaultFolders(baseDir string) DetectedInstallers {
	var result DetectedInstallers
	var dcodeCandidates []string
	var paCandidates []string
	var ufedCandidates []string

	scanDir := func(dir string, maxPADepth, maxDCodeDepth int, skipChildDir string) {
		cleanDir := filepath.Clean(dir)
		cleanSkip := ""
		if skipChildDir != "" {
			cleanSkip = filepath.Clean(skipChildDir)
		}
		baseDepth := strings.Count(cleanDir, string(filepath.Separator))

		_ = filepath.WalkDir(cleanDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				return nil
			}

			cleanPath := filepath.Clean(path)
			if cleanSkip != "" && cleanPath == cleanSkip {
				return filepath.SkipDir
			}

			currentDepth := strings.Count(cleanPath, string(filepath.Separator)) - baseDepth
			if currentDepth > maxDCodeDepth {
				return filepath.SkipDir
			}

			nameLower := strings.ToLower(d.Name())
			if isSkippedFolder(nameLower) {
				return filepath.SkipDir
			}

			entries, err := os.ReadDir(path)
			if err != nil {
				return nil
			}

			var has7z, hasBin bool
			var exes []string

			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				eLower := strings.ToLower(e.Name())
				if strings.HasSuffix(eLower, ".7z") {
					has7z = true
				} else if strings.HasSuffix(eLower, ".bin") {
					hasBin = true
				} else if strings.HasSuffix(eLower, ".exe") {
					exes = append(exes, e.Name())
				}
			}

			// DCode search up to maxDCodeDepth
			if currentDepth <= maxDCodeDepth {
				for _, exe := range exes {
					if isDCodeInstaller(exe) {
						dcodeCandidates = append(dcodeCandidates, filepath.Join(path, exe))
					}
				}
			}

			// PA and UFED search up to maxPADepth
			if currentDepth <= maxPADepth {
				pa, ufed := matchPAAndUFED(path, exes, has7z, hasBin)
				if pa != "" {
					paCandidates = append(paCandidates, pa)
				}
				if ufed != "" {
					ufedCandidates = append(ufedCandidates, ufed)
				}
			}

			return nil
		})
	}

	// 1. Scan the base tool directory and its subfolders
	scanDir(baseDir, 4, 7, "")

	// 2. Scan the parent directory (and sibling folders), skipping the already scanned baseDir
	parentDir := filepath.Dir(filepath.Clean(baseDir))
	if parentDir != "" && parentDir != filepath.Clean(baseDir) && !isSystemRoot(parentDir) {
		scanDir(parentDir, 4, 7, baseDir)
	}

	if len(dcodeCandidates) > 0 {
		sortCandidates(dcodeCandidates)
		result.DCodePath = dcodeCandidates[len(dcodeCandidates)-1]
	}
	if len(paCandidates) > 0 {
		sortCandidates(paCandidates)
		result.PAPath = paCandidates[len(paCandidates)-1]
	}
	if len(ufedCandidates) > 0 {
		sortCandidates(ufedCandidates)
		result.UFEDPath = ufedCandidates[len(ufedCandidates)-1]
	}

	return result
}

func main() {
	toolDir = getToolDirectory()

	var initialDCode, initialPA, initialUFED string

	// Auto-detect installers via default scan (depth 4 for PA/UFED, depth 7 for DCode)
	var autoDetectedDCode, autoDetectedPA, autoDetectedUFED bool
	if initialDCode == "" || initialPA == "" || initialUFED == "" {
		detected := scanDefaultFolders(toolDir)
		if initialDCode == "" && detected.DCodePath != "" {
			initialDCode = detected.DCodePath
			autoDetectedDCode = true
		}
		if initialPA == "" && detected.PAPath != "" {
			initialPA = detected.PAPath
			autoDetectedPA = true
		}
		if initialUFED == "" && detected.UFEDPath != "" {
			initialUFED = detected.UFEDPath
			autoDetectedUFED = true
		}
	}

	// Load exact 64x64 scaled bitmaps (fits cleanly in fixed 84px header)
	dcodeCustom := filepath.Join(toolDir, "dcode_logo.png")
	paCustom := filepath.Join(toolDir, "pa_logo.png")
	ufedCustom := filepath.Join(toolDir, "ufed_logo.png")
	dcodeBitmap = loadExactBitmap(dcodeCustom, defaultDCodeLogoBytes, 64)
	paBitmap = loadExactBitmap(paCustom, defaultPALogoBytes, 64)
	ufedBitmap = loadExactBitmap(ufedCustom, defaultUFEDLogoBytes, 64)

	appFont := Font{Family: "Segoe UI", PointSize: 9}
	boldFont := Font{Family: "Segoe UI", PointSize: 9, Bold: true}
	headerFont := Font{Family: "Segoe UI", PointSize: 13, Bold: true}
	monoFont := Font{Family: "Consolas", PointSize: 9}

	boldFontObj, _ = walk.NewFont("Segoe UI", 9, walk.FontBold)

	if err := (MainWindow{
		AssignTo: &mw,
		Title:    "Inseyets Installer",
		Icon:     "APP", // go-winres assigns the resource name "APP" by default
		MinSize:  Size{Width: 920, Height: 680},
		Size:     Size{Width: 980, Height: 720},
		Layout:   VBox{Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}, Spacing: 6},
		Font:     appFont,
		Children: []Widget{
			// Top Banner: Title + Admin Warning & Elevation Button + Detection Status line on right
			Composite{
				MinSize: Size{Height: 28},
				MaxSize: Size{Height: 28},
				Layout:  HBox{MarginsZero: true, Spacing: 6},
				Children: []Widget{
					Label{
						Text: "Inseyets Installer",
						Font: headerFont,
					},
					HSpacer{},
					CustomWidget{
						AssignTo:            &cwDetectStatus,
						MinSize:             Size{Width: 260, Height: 24},
						MaxSize:             Size{Width: 320, Height: 24},
						InvalidatesOnResize: true,
						PaintPixels: func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
							if detectStatusText != "" && boldFontObj != nil {
								bounds := cwDetectStatus.ClientBoundsPixels()
								bounds.Width -= 4 // padding from the right edge
								return canvas.DrawTextPixels(
									detectStatusText,
									boldFontObj,
									walk.RGB(235, 115, 0),
									bounds,
									walk.TextVCenter|walk.TextRight|walk.TextSingleLine,
								)
							}
							return nil
						},
					},
				},
			},

			// Main Split: 1-DCode (left), 2-Inseyets.PA (middle), 3-Inseyets.UFED (right)
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 10, Alignment: AlignHNearVNear},
				Children: []Widget{
					// Column 1: 1-DCode
					GroupBox{
						Title:  "1-DCode",
						Font:   boldFont,
						Layout: VBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 8}, Spacing: 4},
						Children: []Widget{
							// Fixed-height logo container (84px high)
							Composite{
								MinSize: Size{Height: 84},
								MaxSize: Size{Height: 84},
								Layout:  HBox{MarginsZero: true},
								Children: []Widget{
									HSpacer{},
									ImageView{
										AssignTo: &ivDCodeLogo,
										MinSize:  Size{Width: 64, Height: 64},
										MaxSize:  Size{Width: 64, Height: 64},
										Mode:     ImageViewModeZoom,
									},
									HSpacer{},
								},
							},

							// Executable row: Browse and Clear buttons directly adjacent to label
							Composite{
								MinSize: Size{Height: 24},
								MaxSize: Size{Height: 24},
								Layout:  HBox{MarginsZero: true, Spacing: 6},
								Children: []Widget{
									Label{
										Text: "Installer Executable (.exe):",
										Font: boldFont,
									},
									PushButton{
										AssignTo: &btnDCodeBrowse,
										Text:     "Browse...",
										MaxSize:  Size{Width: 70, Height: 23},
										OnClicked: func() {
											dlg := new(walk.FileDialog)
											dlg.Title = "Select 1-DCode Installer Executable"
											dlg.Filter = "Executable Files (*.exe)|*.exe|All Files (*.*)|*.*"
											if ok, err := dlg.ShowOpen(mw); err == nil && ok {
												leDCodePath.SetText(dlg.FilePath)
											}
										},
									},
									PushButton{
										AssignTo: &btnDCodeClear,
										Text:     "Clear",
										MaxSize:  Size{Width: 45, Height: 23},
										OnClicked: func() {
											leDCodePath.SetText("")
										},
									},
									HSpacer{},
								},
							},
							// Full-width LineEdit directly beneath Browse button
							LineEdit{
								AssignTo: &leDCodePath,
								Text:     initialDCode,
								Font:     appFont,
								MinSize:  Size{Height: 23},
								MaxSize:  Size{Height: 23},
								OnTextChanged: func() {
									updateStartButtonState()
									updateScanAndDetectState()
								},
							},

							// Spacer ABOVE Options: expands simultaneously when window is enlarged
							VSpacer{Size: 14},

							// Options square: Centered horizontally, matching PA and UFED
							Composite{
								Layout: HBox{MarginsZero: true},
								Children: []Widget{
									HSpacer{},
									GroupBox{
										Title:   "Options",
										Font:    boldFont,
										MinSize: Size{Width: 235, Height: 142},
										MaxSize: Size{Width: 250, Height: 155},
										Layout:  VBox{Margins: Margins{Left: 12, Top: 14, Right: 12, Bottom: 10}, Spacing: 4, Alignment: AlignHNearVNear},
										Children: []Widget{
											Label{
												Text: "DCode is a free date-time decoder\nfrom Digital Detective.",
												Font: appFont,
											},
											VSpacer{Size: 4},
											Label{
												Text: "Available for free at:",
												Font: boldFont,
											},
											LinkLabel{
												Font: appFont,
												Text: `<a href="https://www.digital-detective.net/dcode/">digital-detective.net/dcode/</a>`,
												OnLinkActivated: func(link *walk.LinkLabelLink) {
													url := link.URL()
													if url == "" {
														url = "https://www.digital-detective.net/dcode/"
													}
													urlPtr, _ := syscall.UTF16PtrFromString(url)
													verbPtr, _ := syscall.UTF16PtrFromString("open")
													_ = windows.ShellExecute(0, verbPtr, urlPtr, nil, nil, windows.SW_SHOWNORMAL)
												},
											},
											VSpacer{},
										},
									},
									HSpacer{},
								},
							},

							// Spacer BELOW Options: expands simultaneously when window is enlarged
							VSpacer{Size: 14},
						},
					},

					// Column 2: 2-Inseyets.PA
					GroupBox{
						Title:  "2-Inseyets.PA",
						Font:   boldFont,
						Layout: VBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 8}, Spacing: 4},
						Children: []Widget{
							// Fixed-height logo container (84px high)
							Composite{
								MinSize: Size{Height: 84},
								MaxSize: Size{Height: 84},
								Layout:  HBox{MarginsZero: true},
								Children: []Widget{
									HSpacer{},
									ImageView{
										AssignTo: &ivPALogo,
										MinSize:  Size{Width: 64, Height: 64},
										MaxSize:  Size{Width: 64, Height: 64},
										Mode:     ImageViewModeZoom,
									},
									HSpacer{},
								},
							},

							// Executable row: Browse and Clear buttons directly adjacent to label
							Composite{
								MinSize: Size{Height: 24},
								MaxSize: Size{Height: 24},
								Layout:  HBox{MarginsZero: true, Spacing: 6},
								Children: []Widget{
									Label{
										Text: "Installer Executable (.exe):",
										Font: boldFont,
									},
									PushButton{
										AssignTo: &btnPABrowse,
										Text:     "Browse...",
										MaxSize:  Size{Width: 70, Height: 23},
										OnClicked: func() {
											dlg := new(walk.FileDialog)
											dlg.Title = "Select 2-Inseyets.PA Installer Executable"
											dlg.Filter = "Executable Files (*.exe)|*.exe|All Files (*.*)|*.*"
											if ok, err := dlg.ShowOpen(mw); err == nil && ok {
												lePAPath.SetText(dlg.FilePath)
											}
										},
									},
									PushButton{
										AssignTo: &btnPAClear,
										Text:     "Clear",
										MaxSize:  Size{Width: 45, Height: 23},
										OnClicked: func() {
											lePAPath.SetText("")
										},
									},
									HSpacer{},
								},
							},
							// Full-width LineEdit directly beneath Browse button
							LineEdit{
								AssignTo: &lePAPath,
								Text:     initialPA,
								Font:     appFont,
								MinSize:  Size{Height: 23},
								MaxSize:  Size{Height: 23},
								OnTextChanged: func() {
									updateStartButtonState()
									updateScanAndDetectState()
								},
							},

							// Spacer ABOVE Options: expands simultaneously when window is enlarged
							VSpacer{Size: 14},

							// Options square: Centered horizontally, comfortable height with visible title
							Composite{
								Layout: HBox{MarginsZero: true},
								Children: []Widget{
									HSpacer{},
									GroupBox{
										Title:   "Options",
										Font:    boldFont,
										MinSize: Size{Width: 235, Height: 142},
										MaxSize: Size{Width: 250, Height: 155},
										Layout:  VBox{Margins: Margins{Left: 12, Top: 14, Right: 12, Bottom: 10}, Spacing: 4, Alignment: AlignHNearVNear},
										Children: []Widget{
											CheckBox{
												AssignTo: &cbPACollectLogs,
												Text:     "Collect Logs",
												Font:     boldFont,
												Checked:  false,
											},
											CheckBox{
												AssignTo: &cbPASkipValidations,
												Text:     "Skip Validations",
												Font:     boldFont,
												Checked:  true,
											},
											CheckBox{
												AssignTo: &cbPAInstallCloud,
												Text:     "Install Cloud",
												Font:     boldFont,
												Checked:  true,
											},
											CheckBox{
												AssignTo: &cbPAInstallMaps,
												Text:     "Install Maps",
												Font:     boldFont,
												Checked:  true,
											},
										},
									},
									HSpacer{},
								},
							},

							// Spacer BELOW Options: expands simultaneously when window is enlarged
							VSpacer{Size: 14},
						},
					},

					// Column 3: 3-Inseyets.UFED
					GroupBox{
						Title:  "3-Inseyets.UFED",
						Font:   boldFont,
						Layout: VBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 8}, Spacing: 4},
						Children: []Widget{
							// Fixed-height logo container (84px high, identical to others)
							Composite{
								MinSize: Size{Height: 84},
								MaxSize: Size{Height: 84},
								Layout:  HBox{MarginsZero: true},
								Children: []Widget{
									HSpacer{},
									ImageView{
										AssignTo: &ivUFEDLogo,
										MinSize:  Size{Width: 64, Height: 64},
										MaxSize:  Size{Width: 64, Height: 64},
										Mode:     ImageViewModeZoom,
									},
									HSpacer{},
								},
							},

							// Executable row: Browse and Clear buttons directly adjacent to label
							Composite{
								MinSize: Size{Height: 24},
								MaxSize: Size{Height: 24},
								Layout:  HBox{MarginsZero: true, Spacing: 6},
								Children: []Widget{
									Label{
										Text: "Installer Executable (.exe):",
										Font: boldFont,
									},
									PushButton{
										AssignTo: &btnUFEDBrowse,
										Text:     "Browse...",
										MaxSize:  Size{Width: 70, Height: 23},
										OnClicked: func() {
											dlg := new(walk.FileDialog)
											dlg.Title = "Select 3-Inseyets.UFED Installer Executable"
											dlg.Filter = "Executable Files (*.exe)|*.exe|All Files (*.*)|*.*"
											if ok, err := dlg.ShowOpen(mw); err == nil && ok {
												leUFEDPath.SetText(dlg.FilePath)
											}
										},
									},
									PushButton{
										AssignTo: &btnUFEDClear,
										Text:     "Clear",
										MaxSize:  Size{Width: 45, Height: 23},
										OnClicked: func() {
											leUFEDPath.SetText("")
										},
									},
									HSpacer{},
								},
							},
							// Full-width LineEdit directly beneath Browse button
							LineEdit{
								AssignTo: &leUFEDPath,
								Text:     initialUFED,
								Font:     appFont,
								MinSize:  Size{Height: 23},
								MaxSize:  Size{Height: 23},
								OnTextChanged: func() {
									updateStartButtonState()
									updateScanAndDetectState()
								},
							},

							// Spacer ABOVE Options: expands simultaneously when window is enlarged
							VSpacer{Size: 14},

							// Options square: Centered horizontally, comfortable height matching others exactly
							Composite{
								Layout: HBox{MarginsZero: true},
								Children: []Widget{
									HSpacer{},
									GroupBox{
										Title:   "Options",
										Font:    boldFont,
										MinSize: Size{Width: 235, Height: 142},
										MaxSize: Size{Width: 250, Height: 155},
										Layout:  VBox{Margins: Margins{Left: 12, Top: 14, Right: 12, Bottom: 10}, Spacing: 4, Alignment: AlignHNearVNear},
										Children: []Widget{
											CheckBox{
												AssignTo: &cbUFEDCollectLogs,
												Text:     "Collect Logs",
												Font:     boldFont,
												Checked:  false,
											},
											CheckBox{
												AssignTo: &cbUFEDRestart,
												Text:     "Restart when done",
												Font:     boldFont,
												Checked:  false,
											},
											VSpacer{},
										},
									},
									HSpacer{},
								},
							},

							// Spacer BELOW Options: expands simultaneously when window is enlarged
							VSpacer{Size: 14},
						},
					},
				},
			},

			// Action Bar (Start Installation + Logs Folder + Timer)
			Composite{
				MinSize: Size{Height: 34},
				MaxSize: Size{Height: 34},
				Layout:  HBox{MarginsZero: true, Spacing: 8},
				Children: []Widget{
					PushButton{
						AssignTo: &btnStart,
						Text:     "Start Installation",
						Font:     Font{Family: "Segoe UI", PointSize: 10, Bold: true},
						MinSize:  Size{Width: 150, Height: 32},
						OnClicked: func() {
							startInstallation()
						},
					},
					PushButton{
						AssignTo: &btnOpenFolder,
						Text:     "Logs Folder",
						Font:     appFont,
						MinSize:  Size{Width: 110, Height: 32},
						OnClicked: func() {
							_ = exec.Command("explorer.exe", toolDir).Start()
						},
					},
					HSpacer{},
					Label{
						AssignTo: &lblTimer,
						Text:     "Elapsed: 00:00:00",
						Font:     Font{Family: "Segoe UI", PointSize: 10, Bold: true},
					},
				},
			},

			// Progress Bar & Status
			ProgressBar{
				AssignTo: &pbProgress,
				MinSize:  Size{Height: 16},
				MaxSize:  Size{Height: 16},
			},
			Composite{
				MinSize: Size{Height: 20},
				MaxSize: Size{Height: 20},
				Layout:  HBox{MarginsZero: true},
				Children: []Widget{
					Label{
						AssignTo: &lblStatus,
						Text:     "Status: Ready. Please select at least one installer above.",
						Font:     boldFont,
					},
					HSpacer{},
				},
			},

			// Console Log Viewer (absorbs extra window height expansions smoothly)
			GroupBox{
				Title:  "Live Execution Console & Process Logs",
				Font:   boldFont,
				Layout: VBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 4},
				Children: []Widget{
					TextEdit{
						AssignTo: &teLog,
						ReadOnly: true,
						VScroll:  true,
						Font:     monoFont,
						MinSize:  Size{Height: 100},
					},
					Composite{
						Layout: HBox{MarginsZero: true, Spacing: 8},
						Children: []Widget{
							PushButton{
								AssignTo: &btnClearLog,
								Text:     "Clear Console",
								Font:     appFont,
								OnClicked: func() {
									teLog.SetText("")
								},
							},
							HSpacer{},
							Label{
								Text: "Installer output and monitoring heartbeats will stream here in real time.",
								Font: Font{Family: "Segoe UI", PointSize: 8},
							},
						},
					},
				},
			},
		},
	}.Create()); err != nil {
		fmt.Fprintf(os.Stderr, "Fatal UI error: %v\n", err)
		win.MessageBox(0, syscall.StringToUTF16Ptr(err.Error()), syscall.StringToUTF16Ptr("Inseyets Installer Error"), win.MB_ICONERROR|win.MB_OK)
		return
	}

	// Always center window on screen and bring to front on launch
	centerWindow(mw.Handle())
	bringWindowToFront(mw.Handle())

	// Set exact-sized logo images
	if dcodeBitmap != nil {
		_ = ivDCodeLogo.SetImage(dcodeBitmap)
	}
	if paBitmap != nil {
		_ = ivPALogo.SetImage(paBitmap)
	}
	if ufedBitmap != nil {
		_ = ivUFEDLogo.SetImage(ufedBitmap)
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		if mw != nil {
			mw.Synchronize(func() {
				centerWindow(mw.Handle())
				bringWindowToFront(mw.Handle())
			})
		}
	}()

	// Refresh button and options state
	updateOptionsEnablement()
	updateStartButtonState()
	updateScanAndDetectState()

	mw.Activating().Attach(func() {
		updateOptionsEnablement()
		updateStartButtonState()
		updateScanAndDetectState()
	})

	logMessage("Inseyets Installer initialized.")
	logMessage(fmt.Sprintf("Working Directory: %s", toolDir))
	logMessage("Running elevated (Administrator privileges enforced by application manifest).")

	if autoDetectedDCode {
		logMessage(fmt.Sprintf("[Auto-Detect] Found 1-DCode: %s", initialDCode))
	}
	if autoDetectedPA {
		logMessage(fmt.Sprintf("[Auto-Detect] Found 2-Inseyets.PA: %s", initialPA))
	}
	if autoDetectedUFED {
		logMessage(fmt.Sprintf("[Auto-Detect] Found 3-Inseyets.UFED: %s", initialUFED))
	}
	if autoDetectedDCode && autoDetectedPA && autoDetectedUFED {
		logMessage("[Auto-Detect] All 3 installers auto-identified in folder scan.")
	} else if !autoDetectedDCode && !autoDetectedPA && !autoDetectedUFED && initialDCode == "" && initialPA == "" && initialUFED == "" {
		logMessage("[Auto-Detect] No installers found within search depths (DCode depth 7, PA/UFED depth 4). Please browse manually.")
	}

	// Show confirmation popup on launch ONLY if installers were auto-detected
	if autoDetectedDCode || autoDetectedPA || autoDetectedUFED {
		go func() {
			time.Sleep(200 * time.Millisecond)
			if mw != nil {
				mw.Synchronize(func() {
					var lines []string
					if autoDetectedDCode {
						lines = append(lines, fmt.Sprintf("• 1-DCode: %s", filepath.Base(initialDCode)))
					}
					if autoDetectedPA {
						lines = append(lines, fmt.Sprintf("• 2-Inseyets.PA: %s", filepath.Base(initialPA)))
					}
					if autoDetectedUFED {
						lines = append(lines, fmt.Sprintf("• 3-Inseyets.UFED: %s", filepath.Base(initialUFED)))
					}

					msg := fmt.Sprintf("Installers successfully auto-identified!\n\n%s", strings.Join(lines, "\n"))
					walk.MsgBox(mw, "Installers Auto-Identified", msg, walk.MsgBoxIconInformation)
				})
			}
		}()
	}

	mw.Run()
}

func updateOptionsEnablement() {
	// Options are disabled only while an installation is running
	enabled := !isInstalling

	cbPACollectLogs.SetEnabled(enabled)
	cbPASkipValidations.SetEnabled(enabled)
	cbPAInstallCloud.SetEnabled(enabled)
	cbPAInstallMaps.SetEnabled(enabled)

	cbUFEDCollectLogs.SetEnabled(enabled)
	cbUFEDRestart.SetEnabled(enabled)
}

func getDCodeArgs() (string, []string) {
	exePath := strings.TrimSpace(leDCodePath.Text())
	args := constructDCodeArgs(exePath)
	return exePath, args
}

func getPAArgs() (string, []string) {
	exePath := strings.TrimSpace(lePAPath.Text())
	args := constructPAArgs(
		exePath,
		cbPACollectLogs.Checked(),
		cbPASkipValidations.Checked(),
		cbPAInstallCloud.Checked(),
		cbPAInstallMaps.Checked(),
		toolDir,
	)
	return exePath, args
}

func getUFEDArgs() (string, []string) {
	exePath := strings.TrimSpace(leUFEDPath.Text())
	args := constructUFEDArgs(
		exePath,
		cbUFEDCollectLogs.Checked(),
		cbUFEDRestart.Checked(),
		toolDir,
	)
	return exePath, args
}

func updateStartButtonState() {
	if isInstalling {
		btnStart.SetEnabled(false)
		return
	}

	dcodeSelected := strings.TrimSpace(leDCodePath.Text()) != ""
	paSelected := strings.TrimSpace(lePAPath.Text()) != ""
	ufedSelected := strings.TrimSpace(leUFEDPath.Text()) != ""

	hasAny := dcodeSelected || paSelected || ufedSelected
	btnStart.SetEnabled(hasAny)

	if !hasAny {
		lblStatus.SetText("Status: Ready. Please select at least one installer above.")
		return
	}

	var selectedNames []string
	if dcodeSelected {
		selectedNames = append(selectedNames, "1-DCode")
	}
	if paSelected {
		selectedNames = append(selectedNames, "2-Inseyets.PA")
	}
	if ufedSelected {
		selectedNames = append(selectedNames, "3-Inseyets.UFED")
	}

	if len(selectedNames) == 3 {
		lblStatus.SetText("Status: Ready to install 1-DCode, 2-Inseyets.PA, and 3-Inseyets.UFED sequentially.")
	} else if len(selectedNames) == 2 {
		lblStatus.SetText(fmt.Sprintf("Status: Ready to install %s and %s sequentially.", selectedNames[0], selectedNames[1]))
	} else {
		lblStatus.SetText(fmt.Sprintf("Status: Ready to install %s.", selectedNames[0]))
	}
}

func setInputsEnabled(enabled bool) {
	leDCodePath.SetEnabled(enabled)
	btnDCodeBrowse.SetEnabled(enabled)
	btnDCodeClear.SetEnabled(enabled)

	lePAPath.SetEnabled(enabled)
	btnPABrowse.SetEnabled(enabled)
	btnPAClear.SetEnabled(enabled)

	leUFEDPath.SetEnabled(enabled)
	btnUFEDBrowse.SetEnabled(enabled)
	btnUFEDClear.SetEnabled(enabled)

	if enabled {
		updateScanAndDetectState()
	}

	updateOptionsEnablement()
}

func updateScanAndDetectState() {
	dcodeSet := leDCodePath != nil && strings.TrimSpace(leDCodePath.Text()) != ""
	paSet := lePAPath != nil && strings.TrimSpace(lePAPath.Text()) != ""
	ufedSet := leUFEDPath != nil && strings.TrimSpace(leUFEDPath.Text()) != ""

	count := 0
	if dcodeSet {
		count++
	}
	if paSet {
		count++
	}
	if ufedSet {
		count++
	}

	if count == 3 {
		detectStatusText = "✔ Installers auto-identified"
	} else if count == 2 {
		detectStatusText = "✔ 2 installers auto-identified"
	} else if count == 1 {
		if dcodeSet {
			detectStatusText = "✔ 1-DCode auto-identified"
		} else if paSet {
			detectStatusText = "✔ 2-Inseyets.PA auto-identified"
		} else {
			detectStatusText = "✔ 3-Inseyets.UFED auto-identified"
		}
	} else {
		detectStatusText = ""
	}

	if cwDetectStatus != nil {
		cwDetectStatus.Invalidate()
	}
}

func logMessage(msg string) {
	timestamp := time.Now().Format("15:04:05")
	formatted := fmt.Sprintf("[%s] %s\r\n", timestamp, msg)

	if mw != nil {
		mw.Synchronize(func() {
			teLog.AppendText(formatted)
			teLog.ScrollToCaret()
		})
	}
}

func startInstallation() {
	installMutex.Lock()
	if isInstalling {
		installMutex.Unlock()
		return
	}
	isInstalling = true
	installMutex.Unlock()

	dcodeSelected := strings.TrimSpace(leDCodePath.Text()) != ""
	paSelected := strings.TrimSpace(lePAPath.Text()) != ""
	ufedSelected := strings.TrimSpace(leUFEDPath.Text()) != ""

	if !dcodeSelected && !paSelected && !ufedSelected {
		walk.MsgBox(mw, "Attention", "Please select at least one valid installer executable before starting.", walk.MsgBoxIconWarning)
		isInstalling = false
		updateStartButtonState()
		return
	}

	if dcodeSelected {
		dcodeExe, _ := getDCodeArgs()
		if _, err := os.Stat(dcodeExe); err != nil {
			walk.MsgBox(mw, "File Not Found", fmt.Sprintf("1-DCode installer was not found at:\n%s", dcodeExe), walk.MsgBoxIconError)
			isInstalling = false
			updateStartButtonState()
			return
		}
	}

	if paSelected {
		paExe, _ := getPAArgs()
		if _, err := os.Stat(paExe); err != nil {
			walk.MsgBox(mw, "File Not Found", fmt.Sprintf("2-Inseyets.PA installer was not found at:\n%s", paExe), walk.MsgBoxIconError)
			isInstalling = false
			updateStartButtonState()
			return
		}
	}

	if ufedSelected {
		ufedExe, _ := getUFEDArgs()
		if _, err := os.Stat(ufedExe); err != nil {
			walk.MsgBox(mw, "File Not Found", fmt.Sprintf("3-Inseyets.UFED installer was not found at:\n%s", ufedExe), walk.MsgBoxIconError)
			isInstalling = false
			updateStartButtonState()
			return
		}
	}

	btnStart.SetEnabled(false)
	setInputsEnabled(false)
	_ = pbProgress.SetMarqueeMode(true)

	ctx := context.Background()

	timerStopChan = make(chan struct{})
	startTime := time.Now()
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-timerStopChan:
				return
			case t := <-ticker.C:
				elapsed := t.Sub(startTime)
				h := int(elapsed.Hours())
				m := int(elapsed.Minutes()) % 60
				s := int(elapsed.Seconds()) % 60
				timerText := fmt.Sprintf("Elapsed: %02d:%02d:%02d", h, m, s)
				mw.Synchronize(func() {
					lblTimer.SetText(timerText)
				})
			}
		}
	}()

	go func() {
		defer func() {
			close(timerStopChan)
			installMutex.Lock()
			isInstalling = false
			installMutex.Unlock()

			mw.Synchronize(func() {
				_ = pbProgress.SetMarqueeMode(false)
				setInputsEnabled(true)
				updateStartButtonState()
			})
		}()

		logMessage("==================================================")
		logMessage("Starting Installation Workflow...")
		logMessage(fmt.Sprintf("Working Directory: %s", toolDir))

		totalSteps := 0
		if dcodeSelected {
			totalSteps++
		}
		if paSelected {
			totalSteps++
		}
		if ufedSelected {
			totalSteps++
		}
		currentStep := 0

		// Step 1: 1-DCode (installed before Inseyets.PA)
		if dcodeSelected {
			currentStep++
			stepBanner := fmt.Sprintf("[%d/%d] 1-DCode", currentStep, totalSteps)
			mw.Synchronize(func() {
				lblStatus.SetText(fmt.Sprintf("Status: Installing %s...", stepBanner))
			})

			dcodeExe, dcodeArgs := getDCodeArgs()
			fullCmd := formatCommandLine(dcodeExe, dcodeArgs)
			logMessage(fmt.Sprintf(">>> Step %s", stepBanner))
			logMessage(fmt.Sprintf("Executing: %s", fullCmd))

			success := executeInstallerProcess(ctx, dcodeExe, dcodeArgs, "1-DCode")
			if !success {
				logMessage("ERROR: 1-DCode installation failed or returned an error.")
				mw.Synchronize(func() {
					lblStatus.SetText("Status: 1-DCode installation failed. See logs.")
					walk.MsgBox(mw, "Installation Failed", "1-DCode installer reported an error.\nPlease check the console log for details.", walk.MsgBoxIconError)
				})
				return
			}
			logMessage("SUCCESS: 1-DCode installation completed successfully.")
		}

		// Step 2: 2-Inseyets.PA
		if paSelected {
			currentStep++
			stepBanner := fmt.Sprintf("[%d/%d] 2-Inseyets.PA", currentStep, totalSteps)
			mw.Synchronize(func() {
				lblStatus.SetText(fmt.Sprintf("Status: Installing %s...", stepBanner))
			})

			paExe, paArgs := getPAArgs()
			fullCmd := formatCommandLine(paExe, paArgs)
			logMessage(fmt.Sprintf(">>> Step %s", stepBanner))
			logMessage(fmt.Sprintf("Executing: %s", fullCmd))

			success := executeInstallerProcess(ctx, paExe, paArgs, "2-Inseyets.PA")
			if !success {
				logMessage("ERROR: 2-Inseyets.PA installation failed or returned an error.")
				mw.Synchronize(func() {
					lblStatus.SetText("Status: 2-Inseyets.PA installation failed. See logs.")
					walk.MsgBox(mw, "Installation Failed", "2-Inseyets.PA installer reported an error.\nPlease check the console log and PA_logs folder for details.", walk.MsgBoxIconError)
				})
				return
			}
			logMessage("SUCCESS: 2-Inseyets.PA installation completed successfully.")
		}

		// Step 3: 3-Inseyets.UFED
		if ufedSelected {
			currentStep++
			stepBanner := fmt.Sprintf("[%d/%d] 3-Inseyets.UFED", currentStep, totalSteps)
			mw.Synchronize(func() {
				lblStatus.SetText(fmt.Sprintf("Status: Installing %s...", stepBanner))
			})

			ufedExe, ufedArgs := getUFEDArgs()
			fullCmd := formatCommandLine(ufedExe, ufedArgs)
			logMessage(fmt.Sprintf(">>> Step %s", stepBanner))
			logMessage(fmt.Sprintf("Executing: %s", fullCmd))

			success := executeInstallerProcess(ctx, ufedExe, ufedArgs, "3-Inseyets.UFED")
			if !success {
				logMessage("ERROR: 3-Inseyets.UFED installation failed or returned an error.")
				mw.Synchronize(func() {
					lblStatus.SetText("Status: 3-Inseyets.UFED installation failed. See logs.")
					walk.MsgBox(mw, "Installation Failed", "3-Inseyets.UFED installer reported an error.\nPlease check the console log and UFED_logs folder for details.", walk.MsgBoxIconError)
				})
				return
			}
			logMessage("SUCCESS: 3-Inseyets.UFED installation completed successfully.")
		}

		// All installations completed
		totalElapsed := time.Since(startTime).Round(time.Second)
		logMessage("==================================================")
		logMessage(fmt.Sprintf("ALL INSTALLATIONS COMPLETED SUCCESSFULLY in %s", totalElapsed))

		mw.Synchronize(func() {
			pbProgress.SetRange(0, 100)
			pbProgress.SetValue(100)
			lblStatus.SetText(fmt.Sprintf("Status: All installations completed successfully in %s!", totalElapsed))
			walk.MsgBox(mw, "Installation Completed", fmt.Sprintf("All requested software installations have finished successfully!\n\nTotal Duration: %s", totalElapsed), walk.MsgBoxIconInformation)
		})
	}()
}

func executeInstallerProcess(ctx context.Context, exe string, args []string, label string) bool {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = filepath.Dir(exe)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		logMessage(fmt.Sprintf("[%s] Error binding stdout: %v", label, err))
		return false
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		logMessage(fmt.Sprintf("[%s] Error binding stderr: %v", label, err))
		return false
	}

	if err := cmd.Start(); err != nil {
		logMessage(fmt.Sprintf("[%s] Failed to launch installer: %v", label, err))
		return false
	}

	pid := cmd.Process.Pid
	logMessage(fmt.Sprintf("[%s] Process launched (PID: %d). Installing silently...", label, pid))

	// Heartbeat monitor for long-running silent installers
	stopHeartbeat := make(chan struct{})
	go func() {
		hbTicker := time.NewTicker(60 * time.Second)
		defer hbTicker.Stop()
		hbCount := 0
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-hbTicker.C:
				hbCount++
				logMessage(fmt.Sprintf("[%s Heartbeat] Process PID %d active and running... (%d sec elapsed)", label, pid, hbCount*60))
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// Stream stdout
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				logMessage(fmt.Sprintf("[%s stdout] %s", label, line))
			}
		}
	}()

	// Stream stderr
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				logMessage(fmt.Sprintf("[%s stderr] %s", label, line))
			}
		}
	}()

	wg.Wait()

	waitErr := cmd.Wait()
	close(stopHeartbeat)

	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			if code == 3010 {
				logMessage(fmt.Sprintf("[%s] Installer exited with code 3010 (Reboot required - treated as success).", label))
				return true
			}
			logMessage(fmt.Sprintf("[%s] Installer exited with non-zero code: %d", label, code))
			return false
		}
		logMessage(fmt.Sprintf("[%s] Wait error: %v", label, waitErr))
		return false
	}

	logMessage(fmt.Sprintf("[%s] Installer finished with exit code 0 (Success).", label))
	return true
}
