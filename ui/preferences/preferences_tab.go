//go:build windows

package preferences

import (
	"net"
	"strconv"
	"strings"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	browser "github.com/pkg/browser"
	"github.com/tailscale/walk"
	"github.com/tailscale/win"
)

// PreferencesTab handles the preferences/settings tab
type PreferencesTab struct {
	tabPage             *walk.TabPage
	contentContainer    *walk.Composite
	dnsOverrideCheckBox *walk.CheckBox
	dnsTunnelCheckBox   *walk.CheckBox
	autoConnectCheckBox *walk.CheckBox
	openAtLoginCheckBox *walk.CheckBox
	primaryDNSEdit      *walk.LineEdit
	secondaryDNSEdit    *walk.LineEdit
	mtuEdit             *walk.LineEdit
	saveButton          *walk.PushButton
	configManager       *config.ConfigManager
	window              *PreferencesWindow
}

const (
	minMTU = 576
	maxMTU = 9000
)

// NewPreferencesTab creates a new preferences tab
func NewPreferencesTab(cm *config.ConfigManager) *PreferencesTab {
	return &PreferencesTab{
		configManager: cm,
	}
}

// Create creates the preferences tab UI
func (pt *PreferencesTab) Create(parent *walk.TabWidget) (*walk.TabPage, error) {
	var err error
	if pt.tabPage, err = walk.NewTabPage(); err != nil {
		return nil, err
	}

	pt.tabPage.SetTitle("Preferences")
	pt.tabPage.SetLayout(walk.NewVBoxLayout())

	// Content scrolls so a shorter window doesn't cover the Save button.
	if pt.contentContainer, err = newFormScroll(pt.tabPage); err != nil {
		return nil, err
	}

	// Tip link to docs for settings
	settingsDocLink, err := walk.NewLinkLabel(pt.contentContainer)
	if err != nil {
		return nil, err
	}
	const settingsDocURL = "https://docs.pangolin.net/manage/clients/configure-client"
	settingsDocLink.SetText(`Tip: <a href="` + settingsDocURL + `">See the docs for more information on these settings</a>`)
	settingsDocLink.SetAlignment(walk.AlignHNearVNear)
	settingsDocLink.LinkActivated().Attach(func(link *walk.LinkLabelLink) {
		browser.OpenURL(settingsDocURL)
	})

	// DNS Settings section title
	dnsSectionTitle, err := walk.NewLabel(pt.contentContainer)
	if err != nil {
		return nil, err
	}
	dnsSectionTitle.SetText("DNS Settings")
	font, err := walk.NewFont("Segoe UI", 10, walk.FontBold)
	if err == nil {
		dnsSectionTitle.SetFont(font)
	}

	// DNS Override section
	dnsOverrideContainer, err := walk.NewComposite(pt.contentContainer)
	if err != nil {
		return nil, err
	}
	dnsOverrideLayout := walk.NewVBoxLayout()
	dnsOverrideLayout.SetMargins(walk.Margins{})
	dnsOverrideLayout.SetSpacing(8)
	dnsOverrideContainer.SetLayout(dnsOverrideLayout)

	// DNS Override label and checkbox row
	dnsOverrideRow, err := walk.NewComposite(dnsOverrideContainer)
	if err != nil {
		return nil, err
	}
	dnsOverrideRowLayout := walk.NewHBoxLayout()
	dnsOverrideRowLayout.SetMargins(walk.Margins{})
	dnsOverrideRowLayout.SetSpacing(12)
	dnsOverrideRow.SetLayout(dnsOverrideRowLayout)

	// DNS Override label
	dnsOverrideLabel, err := walk.NewLabel(dnsOverrideRow)
	if err != nil {
		return nil, err
	}
	dnsOverrideLabel.SetText("Enable Aliases (DNS Override)")
	dnsOverrideLabel.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	// DNS Override checkbox
	if pt.dnsOverrideCheckBox, err = walk.NewCheckBox(dnsOverrideRow); err != nil {
		return nil, err
	}
	pt.dnsOverrideCheckBox.SetChecked(pt.configManager.GetDNSOverride()) // Get value from config
	pt.dnsOverrideCheckBox.SetText("")                                   // No text, just the checkbox

	// Spacer
	walk.NewHSpacer(dnsOverrideRow)

	// Description label (below the row)
	descLabel, err := walk.NewLabel(dnsOverrideContainer)
	if err != nil {
		return nil, err
	}
	descLabel.SetText("When enabled, the client uses custom DNS servers to resolve internal\nresources and aliases. This overrides your system’s default DNS settings.\nQueries that cannot be resolved as a Pangolin resource will be forwarded\nto your configured Upstream DNS Server.")
	descLabel.SetTextColor(walk.RGB(100, 100, 100))
	descLabel.SetMinMaxSize(walk.Size{}, walk.Size{Width: 400, Height: 0})

	// DNS Tunnel section
	dnsTunnelContainer, err := walk.NewComposite(pt.contentContainer)
	if err != nil {
		return nil, err
	}
	dnsTunnelLayout := walk.NewVBoxLayout()
	dnsTunnelLayout.SetMargins(walk.Margins{})
	dnsTunnelLayout.SetSpacing(8)
	dnsTunnelContainer.SetLayout(dnsTunnelLayout)

	// DNS Tunnel label and checkbox row
	dnsTunnelRow, err := walk.NewComposite(dnsTunnelContainer)
	if err != nil {
		return nil, err
	}
	dnsTunnelRowLayout := walk.NewHBoxLayout()
	dnsTunnelRowLayout.SetMargins(walk.Margins{})
	dnsTunnelRowLayout.SetSpacing(12)
	dnsTunnelRow.SetLayout(dnsTunnelRowLayout)

	// DNS Tunnel label
	dnsTunnelLabel, err := walk.NewLabel(dnsTunnelRow)
	if err != nil {
		return nil, err
	}
	dnsTunnelLabel.SetText("DNS Over Tunnel")
	dnsTunnelLabel.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	// DNS Tunnel checkbox
	if pt.dnsTunnelCheckBox, err = walk.NewCheckBox(dnsTunnelRow); err != nil {
		return nil, err
	}
	pt.dnsTunnelCheckBox.SetChecked(pt.configManager.GetDNSTunnel()) // Get value from config
	pt.dnsTunnelCheckBox.SetText("")                                 // No text, just the checkbox

	// Spacer
	walk.NewHSpacer(dnsTunnelRow)

	// DNS Tunnel description label (below the row)
	dnsTunnelDescLabel, err := walk.NewLabel(dnsTunnelContainer)
	if err != nil {
		return nil, err
	}
	dnsTunnelDescLabel.SetText("When enabled, DNS queries are routed through the tunnel for\nremote resolution. To ensure queries are tunneled correctly,\nyou must define the DNS server as a Pangolin resource and\nenter its address as an Upstream DNS Server.")
	dnsTunnelDescLabel.SetTextColor(walk.RGB(100, 100, 100))
	dnsTunnelDescLabel.SetMinMaxSize(walk.Size{}, walk.Size{Width: 400, Height: 0})

	// Primary DNS Server section
	primaryDNSContainer, err := walk.NewComposite(pt.contentContainer)
	if err != nil {
		return nil, err
	}
	primaryDNSLayout := walk.NewHBoxLayout()
	primaryDNSLayout.SetMargins(walk.Margins{})
	primaryDNSLayout.SetSpacing(12)
	primaryDNSContainer.SetLayout(primaryDNSLayout)

	primaryDNSLabel, err := walk.NewLabel(primaryDNSContainer)
	if err != nil {
		return nil, err
	}
	primaryDNSLabel.SetText("Primary Upstream DNS Server")
	primaryDNSLabel.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	if pt.primaryDNSEdit, err = walk.NewLineEdit(primaryDNSContainer); err != nil {
		return nil, err
	}
	pt.primaryDNSEdit.SetCueBanner("Default: system DNS")
	pt.primaryDNSEdit.SetText(pt.configManager.GetPrimaryDNS()) // Get value from config

	// Spacer
	walk.NewHSpacer(primaryDNSContainer)

	// Secondary DNS Server section
	secondaryDNSContainer, err := walk.NewComposite(pt.contentContainer)
	if err != nil {
		return nil, err
	}
	secondaryDNSLayout := walk.NewHBoxLayout()
	secondaryDNSLayout.SetMargins(walk.Margins{})
	secondaryDNSLayout.SetSpacing(12)
	secondaryDNSContainer.SetLayout(secondaryDNSLayout)

	secondaryDNSLabel, err := walk.NewLabel(secondaryDNSContainer)
	if err != nil {
		return nil, err
	}
	secondaryDNSLabel.SetText("Secondary Upstream DNS Server")
	secondaryDNSLabel.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	if pt.secondaryDNSEdit, err = walk.NewLineEdit(secondaryDNSContainer); err != nil {
		return nil, err
	}
	pt.secondaryDNSEdit.SetCueBanner("Default: system DNS")
	pt.secondaryDNSEdit.SetText(pt.configManager.GetSecondaryDNS()) // Get value from config

	// Spacer
	walk.NewHSpacer(secondaryDNSContainer)

	// Advanced section title
	advancedSectionTitle, err := walk.NewLabel(pt.contentContainer)
	if err != nil {
		return nil, err
	}
	advancedSectionTitle.SetText("Advanced")
	if font != nil {
		advancedSectionTitle.SetFont(font)
	}

	// MTU section
	mtuContainer, err := walk.NewComposite(pt.contentContainer)
	if err != nil {
		return nil, err
	}
	mtuLayout := walk.NewHBoxLayout()
	mtuLayout.SetMargins(walk.Margins{})
	mtuLayout.SetSpacing(12)
	mtuContainer.SetLayout(mtuLayout)

	mtuLabel, err := walk.NewLabel(mtuContainer)
	if err != nil {
		return nil, err
	}
	mtuLabel.SetText("MTU")
	mtuLabel.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	if pt.mtuEdit, err = walk.NewLineEdit(mtuContainer); err != nil {
		return nil, err
	}
	pt.mtuEdit.SetText(strconv.Itoa(pt.configManager.GetMTU()))

	// Spacer
	walk.NewHSpacer(mtuContainer)

	mtuDescLabel, err := walk.NewLabel(pt.contentContainer)
	if err != nil {
		return nil, err
	}
	mtuDescLabel.SetText("Your sites must be configured to use the same MTU value.")
	mtuDescLabel.SetTextColor(walk.RGB(100, 100, 100))
	mtuDescLabel.SetMinMaxSize(walk.Size{}, walk.Size{Width: 400, Height: 0})

	// Connection section
	connectionSectionTitle, err := walk.NewLabel(pt.contentContainer)
	if err != nil {
		return nil, err
	}
	connectionSectionTitle.SetText("Connection")
	if font != nil {
		connectionSectionTitle.SetFont(font)
	}

	openAtLoginContainer, err := walk.NewComposite(pt.contentContainer)
	if err != nil {
		return nil, err
	}
	openAtLoginLayout := walk.NewVBoxLayout()
	openAtLoginLayout.SetMargins(walk.Margins{})
	openAtLoginLayout.SetSpacing(8)
	openAtLoginContainer.SetLayout(openAtLoginLayout)

	openAtLoginRow, err := walk.NewComposite(openAtLoginContainer)
	if err != nil {
		return nil, err
	}
	openAtLoginRowLayout := walk.NewHBoxLayout()
	openAtLoginRowLayout.SetMargins(walk.Margins{})
	openAtLoginRowLayout.SetSpacing(12)
	openAtLoginRow.SetLayout(openAtLoginRowLayout)

	openAtLoginLabel, err := walk.NewLabel(openAtLoginRow)
	if err != nil {
		return nil, err
	}
	openAtLoginLabel.SetText("Start at login")
	openAtLoginLabel.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	if pt.openAtLoginCheckBox, err = walk.NewCheckBox(openAtLoginRow); err != nil {
		return nil, err
	}
	pt.openAtLoginCheckBox.SetChecked(pt.configManager.GetOpenUIAtLogin() || pt.configManager.GetAutoConnectAtLogin())
	pt.openAtLoginCheckBox.SetText("")

	walk.NewHSpacer(openAtLoginRow)

	openAtLoginDescLabel, err := walk.NewLabel(openAtLoginContainer)
	if err != nil {
		return nil, err
	}
	openAtLoginDescLabel.SetText("Starts Pangolin when you sign in to Windows.")
	openAtLoginDescLabel.SetTextColor(walk.RGB(100, 100, 100))
	openAtLoginDescLabel.SetMinMaxSize(walk.Size{}, walk.Size{Width: 400, Height: 0})

	autoConnectContainer, err := walk.NewComposite(pt.contentContainer)
	if err != nil {
		return nil, err
	}
	autoConnectLayout := walk.NewVBoxLayout()
	autoConnectLayout.SetMargins(walk.Margins{})
	autoConnectLayout.SetSpacing(8)
	autoConnectContainer.SetLayout(autoConnectLayout)

	autoConnectRow, err := walk.NewComposite(autoConnectContainer)
	if err != nil {
		return nil, err
	}
	autoConnectRowLayout := walk.NewHBoxLayout()
	autoConnectRowLayout.SetMargins(walk.Margins{})
	autoConnectRowLayout.SetSpacing(12)
	autoConnectRow.SetLayout(autoConnectRowLayout)

	autoConnectLabel, err := walk.NewLabel(autoConnectRow)
	if err != nil {
		return nil, err
	}
	autoConnectLabel.SetText("Connect at start")
	autoConnectLabel.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	if pt.autoConnectCheckBox, err = walk.NewCheckBox(autoConnectRow); err != nil {
		return nil, err
	}
	pt.autoConnectCheckBox.SetChecked(pt.configManager.GetAutoConnectAtLogin())
	pt.autoConnectCheckBox.SetText("")

	walk.NewHSpacer(autoConnectRow)

	autoConnectDescLabel, err := walk.NewLabel(autoConnectContainer)
	if err != nil {
		return nil, err
	}
	autoConnectDescLabel.SetText("Connects the tunnel whenever Pangolin starts.\nAlso opens Pangolin at sign-in.")
	autoConnectDescLabel.SetTextColor(walk.RGB(100, 100, 100))
	autoConnectDescLabel.SetMinMaxSize(walk.Size{}, walk.Size{Width: 400, Height: 0})

	pt.autoConnectCheckBox.CheckedChanged().Attach(func() {
		if pt.autoConnectCheckBox.Checked() && !pt.openAtLoginCheckBox.Checked() {
			pt.openAtLoginCheckBox.SetChecked(true)
		}
	})
	pt.openAtLoginCheckBox.CheckedChanged().Attach(func() {
		if !pt.openAtLoginCheckBox.Checked() && pt.autoConnectCheckBox.Checked() {
			pt.autoConnectCheckBox.SetChecked(false)
		}
	})

	// Add spacer to fill remaining space
	walk.NewVSpacer(pt.contentContainer)

	// Buttons will be created in AfterAdd() after tab is added to widget tree

	return pt.tabPage, nil
}

// SetWindow sets the parent window reference (called after window creation)
func (pt *PreferencesTab) SetWindow(window *PreferencesWindow) {
	pt.window = window
}

// AfterAdd is called after the tab page is added to the tab widget
func (pt *PreferencesTab) AfterAdd() {
	// Create buttons container after tab is added to widget tree (like old code)
	var err error
	buttonsContainer, err := walk.NewComposite(pt.tabPage)
	if err != nil {
		logger.Error("Failed to create buttons container: %v", err)
		return
	}
	buttonsContainer.SetLayout(walk.NewHBoxLayout())
	buttonsContainer.Layout().SetMargins(walk.Margins{})

	walk.NewHSpacer(buttonsContainer)

	if pt.saveButton, err = walk.NewPushButton(buttonsContainer); err != nil {
		logger.Error("Failed to create save button: %v", err)
		return
	}
	pt.saveButton.SetText("&Save")
	pt.saveButton.Clicked().Attach(func() {
		pt.onSave()
	})

	// When user settings are disabled (e.g. by admin), disable the entire form
	if pt.configManager != nil && pt.configManager.GetUserSettingsDisabled() {
		if pt.contentContainer != nil {
			pt.contentContainer.SetEnabled(false)
		}
		pt.saveButton.SetEnabled(false)
	}
}

// Cleanup cleans up resources when the tab is closed
func (pt *PreferencesTab) Cleanup() {
	// Nothing to clean up for now
}

// isValidIPAddress validates if a string is a valid IP address (IPv4 or IPv6)
func isValidIPAddress(ip string) bool {
	return net.ParseIP(ip) != nil
}

// onSave handles the save button click and saves all DNS settings
func (pt *PreferencesTab) onSave() {
	// Get current values from UI
	dnsOverride := pt.dnsOverrideCheckBox.Checked()
	dnsTunnel := pt.dnsTunnelCheckBox.Checked()
	autoConnect := pt.autoConnectCheckBox.Checked()
	openAtLogin := pt.openAtLoginCheckBox.Checked()
	if autoConnect {
		openAtLogin = true
	}
	primaryDNS := strings.TrimSpace(pt.primaryDNSEdit.Text())
	secondaryDNS := strings.TrimSpace(pt.secondaryDNSEdit.Text())
	mtuText := strings.TrimSpace(pt.mtuEdit.Text())
	mtu, err := strconv.Atoi(mtuText)
	if mtuText == "" || err != nil || mtu < minMTU || mtu > maxMTU {
		// Restore to current config value
		currentValue := strconv.Itoa(pt.configManager.GetMTU())
		pt.mtuEdit.SetText(currentValue)
		var owner walk.Form
		if pt.window != nil {
			owner = pt.window
		}
		td := walk.NewTaskDialog()
		_, _ = td.Show(walk.TaskDialogOpts{
			Owner:         owner,
			Title:         "Invalid Input",
			Content:       "MTU must be a whole number between 576 and 9000.",
			IconSystem:    walk.TaskDialogSystemIconWarning,
			CommonButtons: win.TDCBF_OK_BUTTON,
		})
		return
	}

	// Validate primary DNS is a valid IP address (if provided)
	if primaryDNS != "" && !isValidIPAddress(primaryDNS) {
		// Restore to current config value
		currentValue := pt.configManager.GetPrimaryDNS()
		pt.primaryDNSEdit.SetText(currentValue)
		var owner walk.Form
		if pt.window != nil {
			owner = pt.window
		}
		td := walk.NewTaskDialog()
		_, _ = td.Show(walk.TaskDialogOpts{
			Owner:         owner,
			Title:         "Invalid Input",
			Content:       "Primary DNS Server must be a valid IP address.",
			IconSystem:    walk.TaskDialogSystemIconWarning,
			CommonButtons: win.TDCBF_OK_BUTTON,
		})
		return
	}

	// Validate secondary DNS is a valid IP address (if provided)
	if secondaryDNS != "" && !isValidIPAddress(secondaryDNS) {
		// Restore to current config value
		currentValue := pt.configManager.GetSecondaryDNS()
		if currentValue == "" {
			pt.secondaryDNSEdit.SetText("")
		} else {
			pt.secondaryDNSEdit.SetText(currentValue)
		}
		var owner walk.Form
		if pt.window != nil {
			owner = pt.window
		}
		td := walk.NewTaskDialog()
		_, _ = td.Show(walk.TaskDialogOpts{
			Owner:         owner,
			Title:         "Invalid Input",
			Content:       "Secondary DNS Server must be a valid IP address.",
			IconSystem:    walk.TaskDialogSystemIconWarning,
			CommonButtons: win.TDCBF_OK_BUTTON,
		})
		return
	}

	// Start from current config so we only update DNS fields and preserve others (e.g. defaultServerURL, userSettingsDisabled)
	cfg := pt.configManager.GetConfigCopy()
	if cfg == nil {
		cfg = &config.Config{}
	}

	dnsOverrideVal := dnsOverride
	dnsTunnelVal := dnsTunnel
	autoConnectVal := autoConnect
	mtuVal := mtu
	cfg.DNSOverride = &dnsOverrideVal
	cfg.DNSTunnel = &dnsTunnelVal
	cfg.AutoConnectAtLogin = &autoConnectVal
	openAtLoginVal := openAtLogin
	cfg.OpenUIAtLogin = &openAtLoginVal
	cfg.MTU = &mtuVal
	if primaryDNS != "" {
		cfg.PrimaryDNS = &primaryDNS
	} else {
		cfg.PrimaryDNS = nil
	}
	if secondaryDNS != "" {
		cfg.SecondaryDNS = &secondaryDNS
	} else {
		cfg.SecondaryDNS = nil
	}

	success := pt.configManager.Save(cfg)

	if success {
		if pt.window != nil && pt.window.trayIcon != nil {
			walk.App().Synchronize(func() {
				pt.window.trayIcon.ShowInfo("Settings Saved", "Settings have been saved successfully.")
			})
		}
	} else {
		// Show popup dialog for error
		var owner walk.Form
		if pt.window != nil {
			owner = pt.window
		}
		td := walk.NewTaskDialog()
		_, _ = td.Show(walk.TaskDialogOpts{
			Owner:         owner,
			Title:         "Save Failed",
			Content:       "Failed to save settings. Please try again.",
			IconSystem:    walk.TaskDialogSystemIconError,
			CommonButtons: win.TDCBF_OK_BUTTON,
		})
	}
}
