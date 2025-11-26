//go:build windows

package ui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fosrl/windows/config"

	"github.com/fosrl/newt/logger"
	"github.com/tailscale/walk"
	"github.com/tailscale/win"
)

const (
	maxLogLinesDisplayed = 10000
	// autoScrollThreshold is the number of items from the bottom that triggers auto-scroll
	// If the user is within this many items of the bottom, we'll auto-scroll
	autoScrollThreshold = 10
)

type LogWindow struct {
	*walk.MainWindow
	logView *walk.TableView
	model   *logModel
}

type LogLine struct {
	Stamp time.Time
	Level string
	Line  string
}

var (
	logWindowInstance *LogWindow
	logWindowMutex    sync.Mutex
)

// ShowLogWindow shows the log viewer window (creates if needed, or brings to front)
func ShowLogWindow(owner walk.Form) error {
	logWindowMutex.Lock()
	defer logWindowMutex.Unlock()

	if logWindowInstance != nil {
		// Check if the window is still valid (not closed)
		if logWindowInstance.Handle() != 0 {
			// Focus the existing window using Windows API
			hwnd := logWindowInstance.Handle()
			win.ShowWindow(hwnd, win.SW_RESTORE)
			win.SetForegroundWindow(hwnd)
			return nil
		}
		// Window was closed, clear the reference
		logWindowInstance = nil
	}

	// Create new window
	lw, err := NewLogWindow(owner)
	if err != nil {
		return err
	}

	logWindowInstance = lw

	// Clean up when window closes
	lw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		logWindowMutex.Lock()
		if logWindowInstance == lw {
			logWindowInstance = nil
		}
		logWindowMutex.Unlock()
	})

	lw.SetVisible(true)
	return nil
}

func NewLogWindow(owner walk.Form) (*LogWindow, error) {
	lw := &LogWindow{}

	var err error
	var disposables walk.Disposables
	defer disposables.Treat()

	if lw.MainWindow, err = walk.NewMainWindow(); err != nil {
		return nil, err
	}
	disposables.Add(lw)

	lw.SetTitle("Pangolin Logs")
	lw.SetLayout(walk.NewVBoxLayout())

	if lw.logView, err = walk.NewTableView(lw); err != nil {
		return nil, err
	}
	lw.logView.SetAlternatingRowBG(true)
	lw.logView.SetLastColumnStretched(true)
	lw.logView.SetGridlines(true)

	contextMenu, err := walk.NewMenu()
	if err != nil {
		return nil, err
	}
	lw.logView.AddDisposable(contextMenu)
	copyAction := walk.NewAction()
	copyAction.SetText("&Copy")
	copyAction.SetShortcut(walk.Shortcut{Modifiers: walk.ModControl, Key: walk.KeyC})
	copyAction.Triggered().Attach(lw.onCopy)
	contextMenu.Actions().Add(copyAction)
	lw.ShortcutActions().Add(copyAction)
	selectAllAction := walk.NewAction()
	selectAllAction.SetText("Select &all")
	selectAllAction.SetShortcut(walk.Shortcut{Modifiers: walk.ModControl, Key: walk.KeyA})
	selectAllAction.Triggered().Attach(lw.onSelectAll)
	contextMenu.Actions().Add(selectAllAction)
	lw.ShortcutActions().Add(selectAllAction)
	saveAction := walk.NewAction()
	saveAction.SetText("&Save to file…")
	saveAction.SetShortcut(walk.Shortcut{Modifiers: walk.ModControl, Key: walk.KeyS})
	saveAction.Triggered().Attach(lw.onSave)
	contextMenu.Actions().Add(saveAction)
	lw.ShortcutActions().Add(saveAction)
	lw.logView.SetContextMenu(contextMenu)
	setSelectionStatus := func() {
		copyAction.SetEnabled(len(lw.logView.SelectedIndexes()) > 0)
		selectAllAction.SetEnabled(len(lw.logView.SelectedIndexes()) < len(lw.model.items))
	}
	lw.logView.SelectedIndexesChanged().Attach(setSelectionStatus)

	stampCol := walk.NewTableViewColumn()
	stampCol.SetName("Stamp")
	stampCol.SetTitle("Time")
	stampCol.SetFormat("2006-01-02 15:04:05.000")
	stampCol.SetWidth(180)
	lw.logView.Columns().Add(stampCol)

	levelCol := walk.NewTableViewColumn()
	levelCol.SetName("Level")
	levelCol.SetTitle("Level")
	levelCol.SetWidth(80)
	lw.logView.Columns().Add(levelCol)

	msgCol := walk.NewTableViewColumn()
	msgCol.SetName("Line")
	msgCol.SetTitle("Log message")
	lw.logView.Columns().Add(msgCol)

	lw.model = newLogModel(lw)
	lw.model.RowsReset().Attach(setSelectionStatus)
	lw.logView.SetModel(lw.model)
	setSelectionStatus()

	buttonsContainer, err := walk.NewComposite(lw)
	if err != nil {
		return nil, err
	}
	buttonsContainer.SetLayout(walk.NewHBoxLayout())
	buttonsContainer.Layout().SetMargins(walk.Margins{})

	walk.NewHSpacer(buttonsContainer)

	saveButton, err := walk.NewPushButton(buttonsContainer)
	if err != nil {
		return nil, err
	}
	saveButton.SetText("&Save")
	saveButton.Clicked().Attach(lw.onSave)

	disposables.Spare()

	// Set window icon
	iconsPath := config.GetIconsPath()
	iconPath := filepath.Join(iconsPath, "icon-orange.ico")
	icon, err := walk.NewIconFromFile(iconPath)
	if err != nil {
		logger.Error("Failed to load window icon from %s: %v", iconPath, err)
	} else {
		if err := lw.SetIcon(icon); err != nil {
			logger.Error("Failed to set window icon: %v", err)
		}
	}

	// Set window size after all components are added
	lw.SetSize(walk.Size{Width: 800, Height: 600})

	return lw, nil
}

func (lw *LogWindow) isAtBottom() bool {
	if len(lw.model.items) == 0 {
		return true
	}

	// Check if the last item is visible
	lastIndex := len(lw.model.items) - 1
	if lw.logView.ItemVisible(lastIndex) {
		return true
	}

	// Check if we're within the threshold of the bottom
	// If any of the last N items are visible, consider it "at bottom"
	thresholdStart := lastIndex - autoScrollThreshold
	if thresholdStart < 0 {
		thresholdStart = 0
	}

	for i := thresholdStart; i <= lastIndex; i++ {
		if lw.logView.ItemVisible(i) {
			return true
		}
	}

	return false
}

func (lw *LogWindow) scrollToBottom() {
	if len(lw.model.items) > 0 {
		lw.logView.EnsureItemVisible(len(lw.model.items) - 1)
	}
}

func (lw *LogWindow) onCopy() {
	var logLines strings.Builder
	selectedItemIndexes := lw.logView.SelectedIndexes()
	if len(selectedItemIndexes) == 0 {
		return
	}
	for i := 0; i < len(selectedItemIndexes); i++ {
		logItem := lw.model.items[selectedItemIndexes[i]]
		logLines.WriteString(fmt.Sprintf("%s [%s] %s\r\n",
			logItem.Stamp.Format("2006-01-02 15:04:05.000"),
			logItem.Level,
			logItem.Line))
	}
	walk.Clipboard().SetText(logLines.String())
}

func (lw *LogWindow) onSelectAll() {
	lw.logView.SetSelectedIndexes([]int{-1})
}

func (lw *LogWindow) onSave() {
	fd := walk.FileDialog{
		Filter:   "Text Files (*.txt)|*.txt|All Files (*.*)|*.*",
		FilePath: fmt.Sprintf("pangolin-log-%s.txt", time.Now().Format("2006-01-02T150405")),
		Title:    "Export log to file",
	}

	form := lw.Form()

	if ok, _ := fd.ShowSave(form); !ok {
		return
	}

	if fd.FilterIndex == 1 && !strings.HasSuffix(fd.FilePath, ".txt") {
		fd.FilePath = fd.FilePath + ".txt"
	}

	writeFileWithOverwriteHandling(form, fd.FilePath, func(file *os.File) error {
		for _, item := range lw.model.items {
			line := fmt.Sprintf("%s [%s] %s\r\n",
				item.Stamp.Format("2006-01-02 15:04:05.000"),
				item.Level,
				item.Line)
			if _, err := file.WriteString(line); err != nil {
				return fmt.Errorf("failed to write log line: %w", err)
			}
		}
		return nil
	})
}

type logModel struct {
	walk.ReflectTableModelBase
	lw       *LogWindow
	quit     chan bool
	items    []LogLine
	filePos  int64
	lastSize int64
	mu       sync.Mutex
}

func newLogModel(lw *LogWindow) *logModel {
	mdl := &logModel{
		lw:   lw,
		quit: make(chan bool),
	}

	// Load initial logs
	mdl.loadInitialLogs()

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				mdl.readNewLines()

			case <-mdl.quit:
				return
			}
		}
	}()

	return mdl
}

func (mdl *logModel) loadInitialLogs() {
	logFile := filepath.Join(config.GetLogDir(), "pangolin.log")
	file, err := os.Open(logFile)
	if err != nil {
		// File doesn't exist yet, that's okay
		return
	}
	defer file.Close()

	// Get file size
	info, err := file.Stat()
	if err != nil {
		return
	}
	mdl.lastSize = info.Size()

	// Read last N lines (to avoid loading too much)
	const maxInitialLines = 1000
	lines := make([]string, 0, maxInitialLines)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > maxInitialLines {
			lines = lines[1:] // Keep only last N lines
		}
	}

	// Parse and add lines
	mdl.mu.Lock()
	for _, line := range lines {
		if parsed := parseLogLine(line); parsed != nil {
			mdl.items = append(mdl.items, *parsed)
		}
	}
	mdl.mu.Unlock()

	// Update position to end of file
	mdl.filePos = mdl.lastSize

	mdl.lw.Synchronize(func() {
		mdl.PublishRowsReset()
		mdl.lw.scrollToBottom()
	})
}

func (mdl *logModel) readNewLines() {
	logFile := filepath.Join(config.GetLogDir(), "pangolin.log")
	file, err := os.Open(logFile)
	if err != nil {
		// File doesn't exist yet, that's okay
		return
	}
	defer file.Close()

	// Get current file size
	info, err := file.Stat()
	if err != nil {
		return
	}
	currentSize := info.Size()

	// If file was rotated (size decreased), reset position
	if currentSize < mdl.lastSize {
		mdl.filePos = 0
		mdl.mu.Lock()
		mdl.items = mdl.items[:0] // Clear items
		mdl.mu.Unlock()
	}

	// If no new data, return
	if currentSize <= mdl.filePos {
		mdl.lastSize = currentSize
		return
	}

	// Seek to last read position
	_, err = file.Seek(mdl.filePos, 0)
	if err != nil {
		return
	}

	// Read new lines
	var newItems []LogLine
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if parsed := parseLogLine(line); parsed != nil {
			newItems = append(newItems, *parsed)
		}
	}

	if len(newItems) == 0 {
		mdl.lastSize = currentSize
		mdl.filePos = currentSize
		return
	}

	mdl.mu.Lock()
	isAtBottom := mdl.lw.isAtBottom() && len(mdl.lw.logView.SelectedIndexes()) <= 1

	mdl.items = append(mdl.items, newItems...)
	if len(mdl.items) > maxLogLinesDisplayed {
		mdl.items = mdl.items[len(mdl.items)-maxLogLinesDisplayed:]
	}
	mdl.mu.Unlock()

	mdl.filePos = currentSize
	mdl.lastSize = currentSize

	mdl.lw.Synchronize(func() {
		mdl.PublishRowsReset()
		if isAtBottom {
			mdl.lw.scrollToBottom()
		}
	})
}

// parseLogLine attempts to parse a log line into a LogLine struct
// Supports various log formats:
// - LEVEL: YYYY/MM/DD HH:MM:SS message (pangolin format)
// - [timestamp] [level] message
// - timestamp level message
// - ISO8601 timestamp level message
func parseLogLine(line string) *LogLine {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	// Format 1: LEVEL: YYYY/MM/DD HH:MM:SS message (pangolin format)
	// Example: ERROR: 2025/11/26 11:37:43 Failed to poll OLM status...
	re1 := regexp.MustCompile(`^(\w+):\s+(\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2})\s+(.+)$`)
	if matches := re1.FindStringSubmatch(line); len(matches) == 4 {
		if t, err := parseTimestamp(matches[2]); err == nil {
			return &LogLine{
				Stamp: t,
				Level: matches[1],
				Line:  matches[3],
			}
		}
	}

	// Try to parse common log formats
	// Format 2: [2006-01-02 15:04:05.000] [LEVEL] message
	re2 := regexp.MustCompile(`^\[([^\]]+)\]\s+\[([^\]]+)\]\s+(.+)$`)
	if matches := re2.FindStringSubmatch(line); len(matches) == 4 {
		if t, err := parseTimestamp(matches[1]); err == nil {
			return &LogLine{
				Stamp: t,
				Level: matches[2],
				Line:  matches[3],
			}
		}
	}

	// Format 3: 2006-01-02T15:04:05.000Z level message
	re3 := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?)\s+(\w+)\s+(.+)$`)
	if matches := re3.FindStringSubmatch(line); len(matches) == 4 {
		if t, err := time.Parse(time.RFC3339Nano, matches[1]); err == nil {
			return &LogLine{
				Stamp: t,
				Level: matches[2],
				Line:  matches[3],
			}
		}
	}

	// Format 4: 2006-01-02 15:04:05.000 level message
	re4 := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(\w+)\s+(.+)$`)
	if matches := re4.FindStringSubmatch(line); len(matches) == 4 {
		if t, err := parseTimestamp(matches[1]); err == nil {
			return &LogLine{
				Stamp: t,
				Level: matches[2],
				Line:  matches[3],
			}
		}
	}

	// Format 5: Just timestamp and message (no level)
	re5 := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(.+)$`)
	if matches := re5.FindStringSubmatch(line); len(matches) == 3 {
		if t, err := parseTimestamp(matches[1]); err == nil {
			return &LogLine{
				Stamp: t,
				Level: "INFO",
				Line:  matches[2],
			}
		}
	}

	// Fallback: use current time and entire line as message
	return &LogLine{
		Stamp: time.Now(),
		Level: "UNKNOWN",
		Line:  line,
	}
}

func parseTimestamp(ts string) (time.Time, error) {
	// Try various timestamp formats
	formats := []string{
		"2006/01/02 15:04:05",     // Pangolin format: YYYY/MM/DD HH:MM:SS
		"2006-01-02 15:04:05.000", // ISO with milliseconds
		"2006-01-02 15:04:05",     // ISO without milliseconds
		"2006-01-02T15:04:05.000", // ISO T format with milliseconds
		"2006-01-02T15:04:05",     // ISO T format without milliseconds
		time.RFC3339,              // RFC3339
		time.RFC3339Nano,          // RFC3339Nano
	}

	for _, format := range formats {
		if t, err := time.Parse(format, ts); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse timestamp: %s", ts)
}

func (mdl *logModel) Items() any {
	mdl.mu.Lock()
	defer mdl.mu.Unlock()
	return mdl.items
}

// writeFileWithOverwriteHandling handles file overwrite confirmation
func writeFileWithOverwriteHandling(owner walk.Form, filePath string, writeFunc func(*os.File) error) {
	if _, err := os.Stat(filePath); err == nil {
		// File exists, ask for confirmation
		userAcceptedChan := make(chan bool, 1)
		td := walk.NewTaskDialog()
		opts := walk.TaskDialogOpts{
			Owner:         owner,
			Title:         "File Exists",
			Content:       fmt.Sprintf("The file %s already exists. Do you want to overwrite it?", filepath.Base(filePath)),
			IconSystem:    walk.TaskDialogSystemIconWarning,
			CommonButtons: win.TDCBF_YES_BUTTON | win.TDCBF_NO_BUTTON,
		}
		opts.CommonButtonClicked(win.TDCBF_YES_BUTTON).Attach(func() bool {
			select {
			case userAcceptedChan <- true:
			default:
			}
			return false
		})
		opts.CommonButtonClicked(win.TDCBF_NO_BUTTON).Attach(func() bool {
			select {
			case userAcceptedChan <- false:
			default:
			}
			return false
		})
		td.Show(opts)
		userAccepted := <-userAcceptedChan
		if !userAccepted {
			return
		}
	}

	file, err := os.Create(filePath)
	if err != nil {
		logger.Error("Failed to create file: %v", err)
		td := walk.NewTaskDialog()
		_, _ = td.Show(walk.TaskDialogOpts{
			Owner:         owner,
			Title:         "Save Failed",
			Content:       fmt.Sprintf("Failed to create file: %v", err),
			IconSystem:    walk.TaskDialogSystemIconError,
			CommonButtons: win.TDCBF_OK_BUTTON,
		})
		return
	}
	defer file.Close()

	if err := writeFunc(file); err != nil {
		logger.Error("Failed to write file: %v", err)
		td := walk.NewTaskDialog()
		_, _ = td.Show(walk.TaskDialogOpts{
			Owner:         owner,
			Title:         "Save Failed",
			Content:       fmt.Sprintf("Failed to write file: %v", err),
			IconSystem:    walk.TaskDialogSystemIconError,
			CommonButtons: win.TDCBF_OK_BUTTON,
		})
		return
	}

	td := walk.NewTaskDialog()
	_, _ = td.Show(walk.TaskDialogOpts{
		Owner:         owner,
		Title:         "Save Successful",
		Content:       fmt.Sprintf("Log saved to %s", filePath),
		IconSystem:    walk.TaskDialogSystemIconInformation,
		CommonButtons: win.TDCBF_OK_BUTTON,
	})
}
