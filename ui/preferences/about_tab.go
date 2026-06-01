//go:build windows

package preferences

import (
	"fmt"
	"time"

	"github.com/fosrl/windows/version"

	browser "github.com/pkg/browser"
	"github.com/tailscale/walk"
)

// AboutTab handles the About tab
type AboutTab struct {
	tabPage *walk.TabPage
}

// NewAboutTab creates a new About tab
func NewAboutTab() *AboutTab {
	return &AboutTab{}
}

// Create creates the About tab UI
func (at *AboutTab) Create(parent *walk.TabWidget) (*walk.TabPage, error) {
	var err error
	if at.tabPage, err = walk.NewTabPage(); err != nil {
		return nil, err
	}

	at.tabPage.SetTitle("About")
	at.tabPage.SetLayout(walk.NewVBoxLayout())

	// Content container with padding
	contentContainer, err := walk.NewComposite(at.tabPage)
	if err != nil {
		return nil, err
	}
	contentLayout := walk.NewVBoxLayout()
	contentLayout.SetMargins(walk.Margins{})
	contentLayout.SetSpacing(16)
	contentContainer.SetLayout(contentLayout)

	// Application section
	appSectionLabel, err := walk.NewLabel(contentContainer)
	if err != nil {
		return nil, err
	}
	appSectionLabel.SetText("Application")
	sectionFont, err := walk.NewFont("Segoe UI", 10, walk.FontBold)
	if err == nil {
		appSectionLabel.SetFont(sectionFont)
	}

	// Application info container
	appInfoContainer, err := walk.NewComposite(contentContainer)
	if err != nil {
		return nil, err
	}
	appInfoLayout := walk.NewVBoxLayout()
	appInfoLayout.SetMargins(walk.Margins{})
	appInfoLayout.SetSpacing(8)
	appInfoContainer.SetLayout(appInfoLayout)

	// Version row
	versionRow, err := walk.NewComposite(appInfoContainer)
	if err != nil {
		return nil, err
	}
	versionRowLayout := walk.NewHBoxLayout()
	versionRowLayout.SetMargins(walk.Margins{})
	versionRowLayout.SetSpacing(12)
	versionRow.SetLayout(versionRowLayout)

	versionLabel, err := walk.NewLabel(versionRow)
	if err != nil {
		return nil, err
	}
	versionLabel.SetText("Version")
	versionLabel.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	versionValueLabel, err := walk.NewLabel(versionRow)
	if err != nil {
		return nil, err
	}
	versionValueLabel.SetText(version.Number)
	versionValueLabel.SetTextColor(walk.RGB(100, 100, 100))

	walk.NewHSpacer(versionRow)

	// Copyright row
	copyrightRow, err := walk.NewComposite(appInfoContainer)
	if err != nil {
		return nil, err
	}
	copyrightRowLayout := walk.NewHBoxLayout()
	copyrightRowLayout.SetMargins(walk.Margins{})
	copyrightRowLayout.SetSpacing(12)
	copyrightRow.SetLayout(copyrightRowLayout)

	copyrightLabel, err := walk.NewLabel(copyrightRow)
	if err != nil {
		return nil, err
	}
	copyrightLabel.SetText("Copyright")
	copyrightLabel.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	year := time.Now().Year()
	copyrightText := fmt.Sprintf("© %d Fossorial, Inc.", year)
	copyrightValueLabel, err := walk.NewLabel(copyrightRow)
	if err != nil {
		return nil, err
	}
	copyrightValueLabel.SetText(copyrightText)
	copyrightValueLabel.SetTextColor(walk.RGB(100, 100, 100))

	walk.NewHSpacer(copyrightRow)

	// Resources section
	resourcesSectionLabel, err := walk.NewLabel(contentContainer)
	if err != nil {
		return nil, err
	}
	resourcesSectionLabel.SetText("Resources")
	if sectionFont != nil {
		resourcesSectionLabel.SetFont(sectionFont)
	}
	resourcesSectionLabel.SetAlignment(walk.AlignHNearVNear)

	// Documentation link
	docLinkLabel, err := walk.NewLinkLabel(contentContainer)
	if err != nil {
		return nil, err
	}
	docLinkLabel.SetText(`<a href="https://docs.pangolin.net/">Documentation</a>`)
	docLinkLabel.SetAlignment(walk.AlignHNearVNear)
	docLinkLabel.LinkActivated().Attach(func(link *walk.LinkLabelLink) {
		browser.OpenURL("https://docs.pangolin.net/")
	})

	// How Pangolin Works link
	howItWorksLinkLabel, err := walk.NewLinkLabel(contentContainer)
	if err != nil {
		return nil, err
	}
	howItWorksLinkLabel.SetText(`<a href="https://docs.pangolin.net/about/how-pangolin-works">How Pangolin Works</a>`)
	howItWorksLinkLabel.SetAlignment(walk.AlignHNearVNear)
	howItWorksLinkLabel.LinkActivated().Attach(func(link *walk.LinkLabelLink) {
		browser.OpenURL("https://docs.pangolin.net/about/how-pangolin-works")
	})

	// Legal section
	legalSectionLabel, err := walk.NewLabel(contentContainer)
	if err != nil {
		return nil, err
	}
	legalSectionLabel.SetText("Legal")
	if sectionFont != nil {
		legalSectionLabel.SetFont(sectionFont)
	}
	legalSectionLabel.SetAlignment(walk.AlignHNearVNear)

	// Terms of Service link
	termsLinkLabel, err := walk.NewLinkLabel(contentContainer)
	if err != nil {
		return nil, err
	}
	termsLinkLabel.SetText(`<a href="https://pangolin.net/tos">Terms of Service</a>`)
	termsLinkLabel.SetAlignment(walk.AlignHNearVNear)
	termsLinkLabel.LinkActivated().Attach(func(link *walk.LinkLabelLink) {
		browser.OpenURL("https://pangolin.net/tos")
	})

	// Privacy Policy link
	privacyLinkLabel, err := walk.NewLinkLabel(contentContainer)
	if err != nil {
		return nil, err
	}
	privacyLinkLabel.SetText(`<a href="https://pangolin.net/privacy">Privacy Policy</a>`)
	privacyLinkLabel.SetAlignment(walk.AlignHNearVNear)
	privacyLinkLabel.LinkActivated().Attach(func(link *walk.LinkLabelLink) {
		browser.OpenURL("https://pangolin.net/privacy")
	})

	// Add spacer to fill remaining space
	walk.NewVSpacer(contentContainer)

	return at.tabPage, nil
}

// AfterAdd is called after the tab page is added to the tab widget
func (at *AboutTab) AfterAdd() {
	// Nothing to do for About tab
}

// Cleanup cleans up resources when the tab is closed
func (at *AboutTab) Cleanup() {
	// Nothing to clean up for About tab
}

