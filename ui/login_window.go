//go:build windows

package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/fosrl/windows/api"
	"github.com/fosrl/windows/auth"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/managers"
	"github.com/fosrl/windows/tunnel"

	"github.com/fosrl/newt/logger"
	browser "github.com/pkg/browser"
	"github.com/tailscale/walk"
	. "github.com/tailscale/walk/declarative"
	"github.com/tailscale/win"
	"golang.org/x/sys/windows"
)

type hostingOption int

const (
	hostingNone hostingOption = iota
	hostingCloud
	hostingSelfHosted
)

type loginState int

const (
	stateHostingSelection loginState = iota
	stateReadyToLogin
	stateDeviceAuthCode
	stateSuccess
)

var (
	openLoginDialog      *walk.Dialog
	openLoginDialogMutex sync.Mutex
)

// normalizeURL ensures the URL has a protocol prefix, defaulting to https:// if none is provided
func normalizeURL(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return url
	}
	// Remove trailing slashes
	url = strings.TrimRight(url, "/")
	// Check if URL already has a protocol
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		// Add https:// prefix if no protocol is present
		url = "https://" + url
	}
	return url
}

// Checks relative to executable first, then falls back to installed location
func getIconsPath() string {
	// Try relative to executable first
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		iconsPath := filepath.Join(exeDir, "icons")
		if _, err := os.Stat(iconsPath); err == nil {
			return iconsPath
		}
		// Also try parent directory (if running from build/)
		parentIconsPath := filepath.Join(filepath.Dir(exeDir), "icons")
		if _, err := os.Stat(parentIconsPath); err == nil {
			return parentIconsPath
		}
	}
	// Fall back to installed location
	return config.GetIconsPath()
}

// isDarkMode detects if Windows is in dark mode
func isDarkMode() bool {
	var key windows.Handle
	keyPath := windows.StringToUTF16Ptr(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`)
	err := windows.RegOpenKeyEx(windows.HKEY_CURRENT_USER, keyPath, 0, windows.KEY_READ, &key)
	if err != nil {
		// Default to light mode if we can't detect
		return false
	}
	defer windows.RegCloseKey(key)

	var value uint32
	var valueLen uint32 = 4
	valueName := windows.StringToUTF16Ptr("AppsUseLightTheme")
	err = windows.RegQueryValueEx(key, valueName, nil, nil, (*byte)(unsafe.Pointer(&value)), &valueLen)
	if err != nil {
		// Default to light mode if we can't read the value
		return false
	}

	// AppsUseLightTheme: 0 = dark mode, 1 = light mode
	return value == 0
}

// ShowLoginDialog shows the login dialog with full authentication flow
func ShowLoginDialog(
	parent walk.Form,
	authManager *auth.AuthManager,
	configManager *config.ConfigManager,
	accountManager *config.AccountManager,
	apiClient *api.APIClient,
	tunnelManager *tunnel.Manager,
) {
	// Check if a login dialog is already open
	openLoginDialogMutex.Lock()
	if openLoginDialog != nil {
		// Check if the dialog is still valid (not closed)
		if openLoginDialog.Handle() != 0 {
			// Focus the existing dialog using Windows API
			hwnd := openLoginDialog.Handle()
			win.ShowWindow(hwnd, win.SW_RESTORE)
			win.SetForegroundWindow(hwnd)
			openLoginDialogMutex.Unlock()
			return
		}
		// Dialog was closed, clear the reference
		openLoginDialog = nil
	}
	openLoginDialogMutex.Unlock()

	var dlg *walk.Dialog
	var contentComposite *walk.Composite
	var buttonComposite *walk.Composite

	activeAccount, _ := accountManager.ActiveAccount()

	// State variables
	currentState := stateHostingSelection
	hostingOpt := hostingNone
	selfHostedURL := ""
	isLoggingIn := false
	hasAutoOpenedBrowser := false
	loginSucceeded := false
	// Initialize temporary hostname from config (will be used for login flow, only persisted after successful login)
	temporaryHostname := config.DefaultHostname
	if activeAccount != nil {
		temporaryHostname = activeAccount.Hostname
	}

	// Context for canceling polling goroutine and login operation
	pollCtx, cancelPoll := context.WithCancel(context.Background())
	loginCtx, cancelLogin := context.WithCancel(context.Background())

	// UI components
	var cloudButton, selfHostedButton *walk.PushButton
	var urlLabel, hintLabel *walk.Label
	var urlLineEdit *walk.LineEdit
	var codeLabel *walk.Label
	var copyButton, openBrowserButton *walk.PushButton
	var manualURLLabel *walk.Label
	var manualURLComposite *walk.Composite
	var progressBar *walk.ProgressBar
	var backButton, cancelButton, loginButton *walk.PushButton
	var logoContainer *walk.Composite
	var termsLabel, andLabel *walk.Label
	var termsLinkLabel, privacyLinkLabel *walk.LinkLabel
	var termsComposite *walk.Composite

	isReadyToLogin := func() bool {
		switch hostingOpt {
		case hostingCloud:
			return true
		case hostingSelfHosted:
			return strings.TrimSpace(selfHostedURL) != ""
		default:
			return false
		}
	}

	updateButtons := func() {
		walk.App().Synchronize(func() {
			showBack := currentState != stateHostingSelection
			showCancel := true
			showLogin := currentState == stateReadyToLogin

			if backButton != nil {
				backButton.SetVisible(showBack)
				backButton.SetEnabled(!isLoggingIn)
			}
			if cancelButton != nil {
				cancelButton.SetVisible(showCancel)
				cancelButton.SetEnabled(true)
			}
			if loginButton != nil {
				loginButton.SetVisible(showLogin)
				loginButton.SetEnabled(!isLoggingIn && isReadyToLogin())
			}
		})
	}

	updateUI := func() {
		walk.App().Synchronize(func() {
			// Show/hide widgets based on state
			showHostingSelection := currentState == stateHostingSelection
			showReadyToLogin := currentState == stateReadyToLogin
			showDeviceAuthCode := currentState == stateDeviceAuthCode

			if cloudButton != nil {
				cloudButton.SetVisible(showHostingSelection)
			}
			if selfHostedButton != nil {
				selfHostedButton.SetVisible(showHostingSelection)
			}

			if urlLabel != nil {
				urlLabel.SetVisible(showReadyToLogin)
			}
			if urlLineEdit != nil {
				urlLineEdit.SetVisible(showReadyToLogin)
			}
			if hintLabel != nil {
				hintLabel.SetVisible(showReadyToLogin)
			}

			if codeLabel != nil {
				codeLabel.SetVisible(showDeviceAuthCode)
			}
			if copyButton != nil {
				copyButton.SetVisible(showDeviceAuthCode)
			}
			if openBrowserButton != nil {
				openBrowserButton.SetVisible(showDeviceAuthCode)
			}
			if manualURLComposite != nil {
				manualURLComposite.SetVisible(showDeviceAuthCode)
			}
			if manualURLLabel != nil {
				manualURLLabel.SetVisible(showDeviceAuthCode)
			}
			if progressBar != nil {
				progressBar.SetVisible(showDeviceAuthCode)
			}

			// Show terms notice only on hosting selection page
			if termsComposite != nil {
				termsComposite.SetVisible(showHostingSelection)
			}

			// Update buttons
			updateButtons()
		})
	}

	updateCodeDisplay := func() {
		walk.App().Synchronize(func() {
			code := authManager.DeviceAuthCode()
			if code != nil && codeLabel != nil {
				// Display code with spaces between characters (PIN style)
				codeStr := *code
				displayCode := strings.Join(strings.Split(codeStr, ""), " ")
				codeLabel.SetText(displayCode)

				// Auto-open browser when code is generated
				if !hasAutoOpenedBrowser {
					hasAutoOpenedBrowser = true
					// Use temporary hostname if set, otherwise fall back to saved hostname
					if temporaryHostname != "" {
						// Remove middle hyphen from code (e.g., "XXXX-XXXX" -> "XXXXXXXX")
						codeWithoutHyphen := strings.ReplaceAll(codeStr, "-", "")
						autoOpenURL := fmt.Sprintf("%s/auth/login/device?code=%s", temporaryHostname, codeWithoutHyphen)
						openBrowser(autoOpenURL)
					}
				}
			}
			// Update manual URL label
			if temporaryHostname != "" && manualURLLabel != nil {
				manualURL := fmt.Sprintf("%s/auth/login/device", temporaryHostname)
				manualURLLabel.SetText(manualURL)
			}
		})
	}

	performLogin := func() {
		// Ensure server URL is configured (but don't persist yet)
		if hostingOpt == hostingSelfHosted {
			url := normalizeURL(selfHostedURL)
			if url == "" {
				walk.App().Synchronize(func() {
					isLoggingIn = false
					currentState = stateReadyToLogin
					updateUI()
					td := walk.NewTaskDialog()
					td.Show(walk.TaskDialogOpts{
						Owner:         dlg,
						Title:         "Error",
						Content:       "Please enter a server URL.",
						IconSystem:    walk.TaskDialogSystemIconError,
						CommonButtons: win.TDCBF_OK_BUTTON,
					})
				})
				return
			}
			temporaryHostname = url
		} else if hostingOpt == hostingCloud {
			temporaryHostname = "https://app.pangolin.net"
		}

		// Pass temporary hostname to login (it will use a temporary API client internally)
		err := authManager.LoginWithDeviceAuth(loginCtx, &temporaryHostname)
		if err != nil {
			// Don't show error dialog if context was canceled (user closed dialog)
			if errors.Is(err, context.Canceled) {
				walk.App().Synchronize(func() {
					isLoggingIn = false
					updateUI()
				})
				return
			}
			walk.App().Synchronize(func() {
				isLoggingIn = false
				errorMsg := err.Error()
				td := walk.NewTaskDialog()
				td.Show(walk.TaskDialogOpts{
					Owner:         dlg,
					Title:         "Login Error",
					Content:       errorMsg,
					IconSystem:    walk.TaskDialogSystemIconError,
					CommonButtons: win.TDCBF_OK_BUTTON,
				})
				hasAutoOpenedBrowser = false
				if hostingOpt == hostingCloud {
					// For cloud, go back to hosting selection
					currentState = stateHostingSelection
					hostingOpt = hostingNone
				} else if hostingOpt == hostingSelfHosted {
					// For self-hosted, go back to URL input stage so user can try again
					// Preserve selfHostedURL and hostingOpt so login button stays enabled
					currentState = stateReadyToLogin
				} else {
					// Fallback to hosting selection
					currentState = stateHostingSelection
					hostingOpt = hostingNone
				}
				updateUI()
			})
			return
		}

		// Success - always stop any running tunnel after login, then close
		logger.Info("Stopping tunnel after successful login")
		if err := managers.IPCClientStopTunnel(); err != nil {
			logger.Error("Failed to stop tunnel after login: %v", err)
			// Still close the dialog even if stopping tunnel fails
		}

		walk.App().Synchronize(func() {
			isLoggingIn = false
			loginSucceeded = true
			dlg.Accept()
		})
	}

	Dialog{
		AssignTo: &dlg,
		Title:    "Login to Pangolin",
		MinSize:  Size{Width: 450, Height: 330},
		MaxSize:  Size{Width: 450, Height: 330},
		Layout:   VBox{Margins: Margins{Left: 20, Top: 10, Right: 20, Bottom: 10}, Spacing: 5},
		Children: []Widget{
			// Logo container at top
			Composite{
				AssignTo: &logoContainer,
				Layout:   HBox{MarginsZero: true, Alignment: AlignHCenterVNear},
				MinSize:  Size{Width: 0, Height: 60},
				MaxSize:  Size{Width: 0, Height: 60},
			},
			// Content area - expands to fill available space and centers its children
			Composite{
				AssignTo: &contentComposite,
				Layout:   VBox{MarginsZero: true, Alignment: AlignHCenterVCenter, Spacing: 4},
				Children: []Widget{
					// Hosting selection buttons
					PushButton{
						AssignTo: &cloudButton,
						Text:     "Pangolin Cloud",
						MinSize:  Size{Width: 300, Height: 40},
						OnClicked: func() {
							hostingOpt = hostingCloud
							// Set temporary hostname for login flow (not persisted until successful login)
							temporaryHostname = "https://app.pangolin.net"

							// Immediately start device auth flow for cloud
							currentState = stateDeviceAuthCode
							isLoggingIn = true
							updateUI()
							go performLogin()
						},
					},
					PushButton{
						AssignTo: &selfHostedButton,
						Text:     "Self-hosted or dedicated instance",
						MinSize:  Size{Width: 300, Height: 40},
						OnClicked: func() {
							hostingOpt = hostingSelfHosted
							currentState = stateReadyToLogin
							updateUI()
						},
					},
					// Self-hosted URL input
					Label{
						AssignTo:  &urlLabel,
						Text:      "Pangolin Server URL",
						Alignment: AlignHCenterVNear,
						Visible:   false,
					},
					LineEdit{
						AssignTo:  &urlLineEdit,
						Text:      selfHostedURL,
						CueBanner: "https://your-server.com",
						MinSize:   Size{Width: 300, Height: 0},
						Visible:   false,
						OnTextChanged: func() {
							if urlLineEdit != nil {
								selfHostedURL = urlLineEdit.Text()
								// Normalize the URL: trim spaces, remove trailing slashes, and add https:// if no protocol
								cleanedURL := normalizeURL(selfHostedURL)

								if cleanedURL != "" {
									temporaryHostname = cleanedURL
								} else {
									temporaryHostname = ""
								}
								updateButtons()
							}
						},
					},
					// Device auth code display
					Label{
						AssignTo:  &codeLabel,
						Text:      "",
						Alignment: AlignHCenterVNear,
						Font:      Font{PointSize: 24, Bold: true},
						Visible:   false,
					},
					Composite{
						Layout: HBox{MarginsZero: true, Spacing: 8, Alignment: AlignHCenterVNear},
						Children: []Widget{
							PushButton{
								AssignTo: &copyButton,
								Text:     "Copy Code",
								Visible:  false,
								OnClicked: func() {
									code := authManager.DeviceAuthCode()
									if code != nil {
										copyToClipboard(*code)
									}
								},
							},
							PushButton{
								AssignTo: &openBrowserButton,
								Text:     "Open Browser",
								Visible:  false,
								OnClicked: func() {
									url := authManager.DeviceAuthLoginURL()
									if url != nil {
										openBrowser(*url)
									}
								},
							},
						},
					},
					Composite{
						AssignTo: &manualURLComposite,
						Layout:   VBox{Margins: Margins{Top: 10}, MarginsZero: true},
						Children: []Widget{
							Label{
								AssignTo:  &manualURLLabel,
								Text:      "",
								Font:      Font{PointSize: 8},
								Alignment: AlignHCenterVCenter,
								Visible:   false,
								TextColor: walk.RGB(0x80, 0x80, 0x80), // Secondary gray color
							},
						},
					},
				},
			},
			// Terms and Privacy notice
			Composite{
				AssignTo: &termsComposite,
				Layout:   HBox{MarginsZero: true, Alignment: AlignHCenterVCenter, Spacing: 0},
				Children: []Widget{
					Label{
						AssignTo:  &termsLabel,
						Text:      "By continuing, you agree to our ",
						Font:      Font{PointSize: 8},
						Alignment: AlignHNearVCenter,
						TextColor: walk.RGB(0x80, 0x80, 0x80), // Secondary gray color
					},
					LinkLabel{
						AssignTo:  &termsLinkLabel,
						Text:      `<a href="https://pangolin.net/terms-of-service.html">Terms of Service</a>`,
						Font:      Font{PointSize: 8},
						Alignment: AlignHNearVCenter,
						OnLinkActivated: func(link *walk.LinkLabelLink) {
							openBrowser("https://pangolin.net/terms-of-service.html")
						},
					},
					Label{
						AssignTo:  &andLabel,
						Text:      " and ",
						Font:      Font{PointSize: 8},
						Alignment: AlignHNearVCenter,
						TextColor: walk.RGB(0x80, 0x80, 0x80), // Secondary gray color
					},
					LinkLabel{
						AssignTo:  &privacyLinkLabel,
						Text:      `<a href="https://pangolin.net/privacy-policy.html">Privacy Policy</a>.`,
						Font:      Font{PointSize: 8},
						Alignment: AlignHNearVCenter,
						OnLinkActivated: func(link *walk.LinkLabelLink) {
							openBrowser("https://pangolin.net/privacy-policy.html")
						},
					},
				},
			},
			// Buttons at bottom
			Composite{
				AssignTo: &buttonComposite,
				Layout:   HBox{MarginsZero: true, Alignment: AlignHFarVNear, Spacing: 8},
				Children: []Widget{
					HSpacer{},
					PushButton{
						AssignTo: &backButton,
						Text:     "Back",
						MinSize:  Size{Width: 75, Height: 0},
						MaxSize:  Size{Width: 75, Height: 0},
						Visible:  false,
						OnClicked: func() {
							if currentState == stateDeviceAuthCode {
								// Cancel the auth flow
								currentState = stateHostingSelection
								hostingOpt = hostingNone
								hasAutoOpenedBrowser = false
							} else {
								currentState = stateHostingSelection
								hostingOpt = hostingNone
								selfHostedURL = ""
								if urlLineEdit != nil {
									urlLineEdit.SetText("")
								}
							}
							updateUI()
						},
					},
					PushButton{
						AssignTo: &cancelButton,
						Text:     "Cancel",
						MinSize:  Size{Width: 75, Height: 0},
						MaxSize:  Size{Width: 75, Height: 0},
						OnClicked: func() {
							dlg.Cancel()
						},
					},
					PushButton{
						AssignTo: &loginButton,
						Text:     "Login",
						MinSize:  Size{Width: 75, Height: 0},
						MaxSize:  Size{Width: 75, Height: 0},
						Visible:  false,
						OnClicked: func() {
							currentState = stateDeviceAuthCode
							isLoggingIn = true
							updateUI()
							go performLogin()
						},
					},
				},
			},
		},
	}.Create(parent)

	// Disable maximize, minimize buttons, and resizing
	style := win.GetWindowLong(dlg.Handle(), win.GWL_STYLE)
	style &^= win.WS_MAXIMIZEBOX
	style &^= win.WS_MINIMIZEBOX
	style &^= win.WS_THICKFRAME // Remove resize border
	win.SetWindowLong(dlg.Handle(), win.GWL_STYLE, style)

	// Make dialog appear in taskbar by setting WS_EX_APPWINDOW extended style
	// and keep it always on top with WS_EX_TOPMOST
	const GWL_EXSTYLE = -20
	const WS_EX_APPWINDOW = 0x00040000
	const WS_EX_TOPMOST = 0x00000008
	exStyle := win.GetWindowLong(dlg.Handle(), GWL_EXSTYLE)
	exStyle |= WS_EX_APPWINDOW
	exStyle |= WS_EX_TOPMOST
	win.SetWindowLong(dlg.Handle(), GWL_EXSTYLE, exStyle)

	// Ensure window stays on top using SetWindowPos
	win.SetWindowPos(dlg.Handle(), win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE)

	// Set fixed size
	dlg.SetSize(walk.Size{Width: 450, Height: 330})

	// Set window icon
	iconsPath := getIconsPath()
	iconPath := filepath.Join(iconsPath, "icon-orange.ico")
	icon, err := walk.NewIconFromFile(iconPath)
	if err != nil {
		logger.Error("Failed to load window icon from %s: %v", iconPath, err)
	} else {
		if err := dlg.SetIcon(icon); err != nil {
			logger.Error("Failed to set window icon: %v", err)
		}
	}

	// Set background color (always light mode)
	bgBrush, _ := walk.NewSolidColorBrush(walk.RGB(0xFC, 0xFC, 0xFC)) // #FCFCFC
	if bgBrush != nil {
		dlg.SetBackground(bgBrush)
		if contentComposite != nil {
			contentComposite.SetBackground(bgBrush)
		}
		if buttonComposite != nil {
			buttonComposite.SetBackground(bgBrush)
		}
		if logoContainer != nil {
			logoContainer.SetBackground(bgBrush)
		}
		if termsComposite != nil {
			termsComposite.SetBackground(bgBrush)
		}
	}

	// Load and display word mark logo
	if logoContainer != nil {
		// Always use black word mark (light mode)
		iconsPath := getIconsPath()
		imagePath := filepath.Join(iconsPath, "word_mark_black.png")

		// Create ImageView widget
		logoImageView, err := walk.NewImageView(logoContainer)
		if err != nil {
			logger.Error("Failed to create ImageView: %v", err)
		} else {
			// Load the image
			img, err := walk.NewImageFromFile(imagePath)
			if err != nil {
				logger.Error("Failed to load word mark image from %s: %v", imagePath, err)
			} else {
				logoImageView.SetImage(img)
			}
		}
	}

	// Initial UI update
	updateUI()

	// Store the dialog reference
	openLoginDialogMutex.Lock()
	openLoginDialog = dlg
	openLoginDialogMutex.Unlock()

	// Clear the dialog reference and cleanup state when it closes
	defer func() {
		// Cancel login operation and polling goroutine
		cancelLogin()
		cancelPoll()

		// Clear device auth state if login didn't succeed
		if !loginSucceeded {
			authManager.ClearDeviceAuth()
			logger.Info("Cleared device auth state after dialog close")
		}

		// Clear dialog reference
		openLoginDialogMutex.Lock()
		if openLoginDialog == dlg {
			openLoginDialog = nil
		}
		openLoginDialogMutex.Unlock()
		logger.Info("Login dialog closed")
	}()

	// Poll for device auth code updates
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-pollCtx.Done():
				// Dialog closed, stop polling
				return
			case <-ticker.C:
				if currentState == stateDeviceAuthCode {
					code := authManager.DeviceAuthCode()
					if code != nil {
						updateCodeDisplay()
					} else if !isLoggingIn {
						// Code was cleared, go back based on hosting option
						walk.App().Synchronize(func() {
							hasAutoOpenedBrowser = false
							if hostingOpt == hostingCloud {
								// For cloud, go back to hosting selection
								currentState = stateHostingSelection
								hostingOpt = hostingNone
							} else if hostingOpt == hostingSelfHosted {
								// For self-hosted, go back to URL input stage so user can try again
								currentState = stateReadyToLogin
							} else {
								// Fallback to hosting selection
								currentState = stateHostingSelection
								hostingOpt = hostingNone
							}
							updateUI()
						})
					}
				}
			}
		}
	}()

	dlg.Run()
}

// openBrowser opens a URL in the default browser
func openBrowser(url string) {
	browser.OpenURL(url)
}

// copyToClipboard copies text to the Windows clipboard
func copyToClipboard(text string) {
	// Open clipboard
	if !win.OpenClipboard(0) {
		logger.Error("Failed to open clipboard")
		return
	}
	defer win.CloseClipboard()

	// Empty clipboard
	win.EmptyClipboard()

	// Convert text to UTF16
	text16, err := windows.UTF16FromString(text)
	if err != nil {
		logger.Error("Failed to convert text to UTF16: %v", err)
		return
	}

	// Allocate global memory
	memSize := len(text16) * 2
	hMem := win.GlobalAlloc(win.GMEM_MOVEABLE, uintptr(memSize))
	if hMem == 0 {
		logger.Error("Failed to allocate memory")
		return
	}
	defer win.GlobalFree(hMem)

	// Lock memory and copy data
	pMem := win.GlobalLock(hMem)
	if pMem == nil {
		logger.Error("Failed to lock memory")
		return
	}
	defer win.GlobalUnlock(hMem)

	copy((*[1 << 20]uint16)(pMem)[:len(text16):len(text16)], text16)

	// Set clipboard data
	if win.SetClipboardData(win.CF_UNICODETEXT, win.HANDLE(hMem)) == 0 {
		logger.Error("Failed to set clipboard data")
		return
	}
}
