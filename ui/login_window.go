//go:build windows

package ui

import (
	"github.com/fosrl/newt/logger"

	"github.com/tailscale/walk"
	. "github.com/tailscale/walk/declarative"
	"github.com/tailscale/win"
)

func ShowLoginDialog(parent walk.Form) {
	var dlg *walk.Dialog
	var acceptPB, cancelPB *walk.PushButton
	var cloudRB, selfHostedRB *walk.RadioButton

	Dialog{
		AssignTo:      &dlg,
		Title:         "Login",
		DefaultButton: &acceptPB,
		CancelButton:  &cancelPB,
		MinSize:       Size{Width: 450, Height: 400},
		MaxSize:       Size{Width: 450, Height: 400},
		Layout:        VBox{MarginsZero: true},
		Children: []Widget{
			Label{
				Text:      "Select login option:",
				Alignment: AlignHNearVNear,
			},
			RadioButtonGroup{
				DataMember: "Option",
				Buttons: []RadioButton{
					RadioButton{
						AssignTo: &cloudRB,
						Text:     "Pangolin Cloud",
					},
					RadioButton{
						AssignTo: &selfHostedRB,
						Text:     "Self-hosted or dedicated instance",
					},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						AssignTo: &acceptPB,
						Text:     "OK",
						OnClicked: func() {
							var selectedOption string
							if cloudRB.Checked() {
								selectedOption = "Pangolin Cloud"
							} else {
								selectedOption = "Self-hosted or dedicated instance"
							}
							logger.Info("Selected option: %s", selectedOption)
							dlg.Accept()
						},
					},
					PushButton{
						AssignTo: &cancelPB,
						Text:     "Cancel",
						OnClicked: func() {
							dlg.Cancel()
						},
					},
				},
			},
		},
	}.Create(parent)

	// Set default selection
	cloudRB.SetChecked(true)

	// Disable maximize and minimize buttons using Windows API
	style := win.GetWindowLong(dlg.Handle(), win.GWL_STYLE)
	style &^= win.WS_MAXIMIZEBOX
	style &^= win.WS_MINIMIZEBOX
	win.SetWindowLong(dlg.Handle(), win.GWL_STYLE, style)

	// Set fixed size
	dlg.SetSize(walk.Size{Width: 450, Height: 400})

	dlg.Run()
}
