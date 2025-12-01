//go:build windows

package preferences

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

// LogsTab handles the logs viewing tab
type LogsTab struct {
	tabPage     *walk.TabPage
	logView     *walk.TableView
	clearButton *walk.PushButton
	saveButton  *walk.PushButton
	model       *logModel
	window      *PreferencesWindow
	mu          sync.Mutex
}

// LogLine represents a single log line
type LogLine struct {
	Stamp time.Time
	Level string
	Line  string
}

// NewLogsTab creates a new logs tab
func NewLogsTab() *LogsTab {
	return &LogsTab{}
}

// Create creates the logs tab UI
func (lt *LogsTab) Create(parent *walk.TabWidget) (*walk.TabPage, error) {
	var err error
	if lt.tabPage, err = walk.NewTabPage(); err != nil {
		return nil, err
	}

	lt.tabPage.SetTitle("Logs")
	lt.tabPage.SetLayout(walk.NewVBoxLayout())

	if lt.logView, err = walk.NewTableView(lt.tabPage); err != nil {
		return nil, err
	}
	lt.logView.SetAlternatingRowBG(true)
	lt.logView.SetLastColumnStretched(true)
	lt.logView.SetGridlines(true)

	contextMenu, err := walk.NewMenu()
	if err != nil {
		return nil, err
	}
	lt.logView.AddDisposable(contextMenu)
	copyAction := walk.NewAction()
	copyAction.SetText("&Copy")
	copyAction.SetShortcut(walk.Shortcut{Modifiers: walk.ModControl, Key: walk.KeyC})
	copyAction.Triggered().Attach(lt.onCopy)
	contextMenu.Actions().Add(copyAction)
	lt.tabPage.ShortcutActions().Add(copyAction)
	selectAllAction := walk.NewAction()
	selectAllAction.SetText("Select &all")
	selectAllAction.SetShortcut(walk.Shortcut{Modifiers: walk.ModControl, Key: walk.KeyA})
	selectAllAction.Triggered().Attach(lt.onSelectAll)
	contextMenu.Actions().Add(selectAllAction)
	lt.tabPage.ShortcutActions().Add(selectAllAction)
	saveAction := walk.NewAction()
	saveAction.SetText("&Save to file…")
	saveAction.SetShortcut(walk.Shortcut{Modifiers: walk.ModControl, Key: walk.KeyS})
	saveAction.Triggered().Attach(lt.onSave)
	contextMenu.Actions().Add(saveAction)
	lt.tabPage.ShortcutActions().Add(saveAction)
	lt.logView.SetContextMenu(contextMenu)
	setSelectionStatus := func() {
		copyAction.SetEnabled(len(lt.logView.SelectedIndexes()) > 0)
		selectAllAction.SetEnabled(len(lt.logView.SelectedIndexes()) < len(lt.model.items))
	}
	lt.logView.SelectedIndexesChanged().Attach(setSelectionStatus)

	stampCol := walk.NewTableViewColumn()
	stampCol.SetName("Stamp")
	stampCol.SetTitle("Time")
	stampCol.SetFormat("2006-01-02 15:04:05.000")
	stampCol.SetWidth(180)
	lt.logView.Columns().Add(stampCol)

	levelCol := walk.NewTableViewColumn()
	levelCol.SetName("Level")
	levelCol.SetTitle("Level")
	levelCol.SetWidth(80)
	lt.logView.Columns().Add(levelCol)

	msgCol := walk.NewTableViewColumn()
	msgCol.SetName("Line")
	msgCol.SetTitle("Log message")
	lt.logView.Columns().Add(msgCol)

	lt.model = newLogModel(lt)
	lt.model.RowsReset().Attach(setSelectionStatus)
	lt.logView.SetModel(lt.model)
	setSelectionStatus()

	// Buttons will be created in AfterAdd() after tab is added to widget tree

	return lt.tabPage, nil
}

// SetWindow sets the parent window reference (called after window creation)
func (lt *LogsTab) SetWindow(window *PreferencesWindow) {
	lt.window = window
}

// AfterAdd is called after the tab page is added to the tab widget
func (lt *LogsTab) AfterAdd() {
	// Create buttons container after tab is added to widget tree (like old code)
	var err error
	buttonsContainer, err := walk.NewComposite(lt.tabPage)
	if err != nil {
		logger.Error("Failed to create buttons container: %v", err)
		return
	}
	buttonsContainer.SetLayout(walk.NewHBoxLayout())
	buttonsContainer.Layout().SetMargins(walk.Margins{})

	walk.NewHSpacer(buttonsContainer)

	if lt.clearButton, err = walk.NewPushButton(buttonsContainer); err != nil {
		logger.Error("Failed to create clear button: %v", err)
		return
	}
	lt.clearButton.SetText("&Clear")
	lt.clearButton.Clicked().Attach(func() {
		lt.onClear()
	})

	if lt.saveButton, err = walk.NewPushButton(buttonsContainer); err != nil {
		logger.Error("Failed to create save button: %v", err)
		return
	}
	lt.saveButton.SetText("&Save")
	lt.saveButton.Clicked().Attach(func() {
		lt.onSave()
	})
}

// Cleanup cleans up resources when the tab is closed
func (lt *LogsTab) Cleanup() {
	if lt.model != nil {
		lt.model.cleanup()
	}
}

func (lt *LogsTab) isAtBottom() bool {
	if len(lt.model.items) == 0 {
		return true
	}

	// Check if the last item is visible
	lastIndex := len(lt.model.items) - 1
	if lt.logView.ItemVisible(lastIndex) {
		return true
	}

	// Check if we're within the threshold of the bottom
	// If any of the last N items are visible, consider it "at bottom"
	thresholdStart := lastIndex - autoScrollThreshold
	if thresholdStart < 0 {
		thresholdStart = 0
	}

	for i := thresholdStart; i <= lastIndex; i++ {
		if lt.logView.ItemVisible(i) {
			return true
		}
	}

	return false
}

func (lt *LogsTab) scrollToBottom() {
	if len(lt.model.items) > 0 {
		lt.logView.EnsureItemVisible(len(lt.model.items) - 1)
	}
}

func (lt *LogsTab) onCopy() {
	var logLines strings.Builder
	selectedItemIndexes := lt.logView.SelectedIndexes()
	if len(selectedItemIndexes) == 0 {
		return
	}
	for i := 0; i < len(selectedItemIndexes); i++ {
		logItem := lt.model.items[selectedItemIndexes[i]]
		logLines.WriteString(fmt.Sprintf("%s [%s] %s\r\n",
			logItem.Stamp.Format("2006-01-02 15:04:05.000"),
			logItem.Level,
			logItem.Line))
	}
	walk.Clipboard().SetText(logLines.String())
}

func (lt *LogsTab) onSelectAll() {
	lt.logView.SetSelectedIndexes([]int{-1})
}

func (lt *LogsTab) onClear() {
	// Clear all log items from the model
	lt.model.mu.Lock()
	lt.model.items = lt.model.items[:0]
	lt.model.mu.Unlock()

	// Update the UI
	walk.App().Synchronize(func() {
		lt.model.PublishRowsReset()
	})
}

func (lt *LogsTab) onSave() {
	fd := walk.FileDialog{
		Filter:   "Text Files (*.txt)|*.txt|All Files (*.*)|*.*",
		FilePath: fmt.Sprintf("pangolin-log-%s.txt", time.Now().Format("2006-01-02T150405")),
		Title:    "Export log to file",
	}

	// Get the parent window for the dialog
	if lt.window == nil {
		return
	}

	if ok, _ := fd.ShowSave(lt.window); !ok {
		return
	}

	if fd.FilterIndex == 1 && !strings.HasSuffix(fd.FilePath, ".txt") {
		fd.FilePath = fd.FilePath + ".txt"
	}

	writeFileWithOverwriteHandling(lt.window, fd.FilePath, func(file *os.File) error {
		for _, item := range lt.model.items {
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
	lt       *LogsTab
	quit     chan bool
	items    []LogLine
	filePos  int64
	lastSize int64
	mu       sync.Mutex
}

func newLogModel(lt *LogsTab) *logModel {
	mdl := &logModel{
		lt:   lt,
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

func (mdl *logModel) cleanup() {
	select {
	case <-mdl.quit:
		// Already closed
	default:
		close(mdl.quit)
	}
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

	walk.App().Synchronize(func() {
		mdl.PublishRowsReset()
		mdl.lt.scrollToBottom()
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
	isAtBottom := mdl.lt.isAtBottom() && len(mdl.lt.logView.SelectedIndexes()) <= 1

	mdl.items = append(mdl.items, newItems...)
	if len(mdl.items) > maxLogLinesDisplayed {
		mdl.items = mdl.items[len(mdl.items)-maxLogLinesDisplayed:]
	}
	mdl.mu.Unlock()

	mdl.filePos = currentSize
	mdl.lastSize = currentSize

	walk.App().Synchronize(func() {
		mdl.PublishRowsReset()
		if isAtBottom {
			mdl.lt.scrollToBottom()
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
