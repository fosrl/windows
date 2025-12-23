//go:build windows

package preferences

import (
	"strings"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"github.com/tailscale/walk"
	"github.com/tailscale/win"
)

// PreferencesTab handles the preferences/settings tab
type PreferencesTab struct {
	tabPage             *walk.TabPage
	dnsOverrideCheckBox *walk.CheckBox
	dnsTunnelCheckBox   *walk.CheckBox
	primaryDNSEdit      *walk.LineEdit
	secondaryDNSEdit    *walk.LineEdit
	saveButton          *walk.PushButton
	configManager       *config.ConfigManager
	window              *PreferencesWindow
}

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

	// Content container - match the structure of logs/olm tabs
	contentContainer, err := walk.NewComposite(pt.tabPage)
	if err != nil {
		return nil, err
	}
	contentLayout := walk.NewVBoxLayout()
	contentLayout.SetMargins(walk.Margins{})
	contentLayout.SetSpacing(16)
	contentContainer.SetLayout(contentLayout)

	// DNS Settings section title
	dnsSectionTitle, err := walk.NewLabel(contentContainer)
	if err != nil {
		return nil, err
	}
	dnsSectionTitle.SetText("DNS Settings")
	font, err := walk.NewFont("Segoe UI", 10, walk.FontBold)
	if err == nil {
		dnsSectionTitle.SetFont(font)
	}

	// DNS Override section
	dnsOverrideContainer, err := walk.NewComposite(contentContainer)
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
	dnsOverrideLabel.SetText("DNS Override")
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
	descLabel.SetText("When enabled, the tunnel uses custom DNS servers to resolve internal resources and aliases. External queries use your configured upstream DNS.")
	descLabel.SetTextColor(walk.RGB(100, 100, 100))

	// DNS Tunnel section
	dnsTunnelContainer, err := walk.NewComposite(contentContainer)
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
	dnsTunnelLabel.SetText("DNS Tunnel")
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
	dnsTunnelDescLabel.SetText("When enabled, DNS queries are sent through the tunnel to a resource. A private resource must be created for the address for it to work and resolve to the correct site.")
	dnsTunnelDescLabel.SetTextColor(walk.RGB(100, 100, 100))

	// Primary DNS Server section
	primaryDNSContainer, err := walk.NewComposite(contentContainer)
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
	pt.primaryDNSEdit.SetText(pt.configManager.GetPrimaryDNS()) // Get value from config

	// Spacer
	walk.NewHSpacer(primaryDNSContainer)

	// Secondary DNS Server section
	secondaryDNSContainer, err := walk.NewComposite(contentContainer)
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
	pt.secondaryDNSEdit.SetText(pt.configManager.GetSecondaryDNS()) // Get value from config

	// Spacer
	walk.NewHSpacer(secondaryDNSContainer)

	// Add spacer to fill remaining space
	walk.NewVSpacer(contentContainer)

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
}

// Cleanup cleans up resources when the tab is closed
func (pt *PreferencesTab) Cleanup() {
	// Nothing to clean up for now
}

// onSave handles the save button click and saves all DNS settings
func (pt *PreferencesTab) onSave() {
	// Get current values from UI
	dnsOverride := pt.dnsOverrideCheckBox.Checked()
	dnsTunnel := pt.dnsTunnelCheckBox.Checked()
	primaryDNS := strings.TrimSpace(pt.primaryDNSEdit.Text())
	secondaryDNS := strings.TrimSpace(pt.secondaryDNSEdit.Text())

	// Validate primary DNS (required)
	if primaryDNS == "" {
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
			Content:       "Primary DNS Server cannot be empty.",
			IconSystem:    walk.TaskDialogSystemIconWarning,
			CommonButtons: win.TDCBF_OK_BUTTON,
		})
		return
	}

	// Get current config and create a copy to modify
	cfg := &config.Config{}

	// Set DNS settings
	dnsOverrideVal := dnsOverride
	dnsTunnelVal := dnsTunnel
	primaryDNSVal := primaryDNS
	cfg.DNSOverride = &dnsOverrideVal
	cfg.DNSTunnel = &dnsTunnelVal
	cfg.PrimaryDNS = &primaryDNSVal
	if secondaryDNS != "" {
		cfg.SecondaryDNS = &secondaryDNS
	} else {
		cfg.SecondaryDNS = nil
	}

	// Save all settings at once
	success := pt.configManager.Save(cfg)

	if success {
		// Show system notification for success
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
