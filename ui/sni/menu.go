//go:build linux

package sni

import (
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// A minimal com.canonical.dbusmenu server for a flat menu of plain items.
// Item IDs are the 1-based index into the menu; 0 is the root.

type menuLayout struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

type menuProps struct {
	ID    int32
	Props map[string]dbus.Variant
}

type menuEvent struct {
	ID        int32
	EventID   string
	Data      dbus.Variant
	Timestamp uint32
}

type menuObject struct {
	items []MenuItem
}

func exportMenu(conn *dbus.Conn, items []MenuItem) error {
	m := &menuObject{items: items}
	if err := conn.Export(m, menuPath, menuIface); err != nil {
		return err
	}
	props, err := prop.Export(conn, menuPath, prop.Map{
		menuIface: {
			"Version":       {Value: uint32(3), Emit: prop.EmitFalse},
			"TextDirection": {Value: "ltr", Emit: prop.EmitFalse},
			"Status":        {Value: "normal", Emit: prop.EmitFalse},
			"IconThemePath": {Value: []string{}, Emit: prop.EmitFalse},
		},
	})
	if err != nil {
		return err
	}
	node := &introspect.Node{
		Name: string(menuPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name:       menuIface,
				Methods:    introspect.Methods(m),
				Properties: props.Introspection(menuIface),
				Signals: []introspect.Signal{
					{Name: "ItemsPropertiesUpdated", Args: []introspect.Arg{{Name: "updatedProps", Type: "a(ia{sv})"}, {Name: "removedProps", Type: "a(ias)"}}},
					{Name: "LayoutUpdated", Args: []introspect.Arg{{Name: "revision", Type: "u"}, {Name: "parent", Type: "i"}}},
					{Name: "ItemActivationRequested", Args: []introspect.Arg{{Name: "id", Type: "i"}, {Name: "timestamp", Type: "u"}}},
				},
			},
		},
	}
	return conn.Export(introspect.NewIntrospectable(node), menuPath, "org.freedesktop.DBus.Introspectable")
}

func (m *menuObject) itemProps(id int32) map[string]dbus.Variant {
	if id == 0 {
		return map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}
	}
	if id < 1 || int(id) > len(m.items) {
		return map[string]dbus.Variant{}
	}
	return map[string]dbus.Variant{
		"label":   dbus.MakeVariant(m.items[id-1].Label),
		"enabled": dbus.MakeVariant(true),
		"visible": dbus.MakeVariant(true),
	}
}

func (m *menuObject) GetLayout(parentID int32, recursionDepth int32, propertyNames []string) (uint32, menuLayout, *dbus.Error) {
	layout := menuLayout{ID: parentID, Props: m.itemProps(parentID), Children: []dbus.Variant{}}
	if parentID == 0 && recursionDepth != 0 {
		for i := range m.items {
			id := int32(i + 1)
			layout.Children = append(layout.Children, dbus.MakeVariant(menuLayout{
				ID: id, Props: m.itemProps(id), Children: []dbus.Variant{},
			}))
		}
	}
	return 1, layout, nil
}

func (m *menuObject) GetGroupProperties(ids []int32, propertyNames []string) ([]menuProps, *dbus.Error) {
	out := make([]menuProps, 0, len(ids))
	for _, id := range ids {
		out = append(out, menuProps{ID: id, Props: m.itemProps(id)})
	}
	return out, nil
}

func (m *menuObject) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	if v, ok := m.itemProps(id)[name]; ok {
		return v, nil
	}
	return dbus.MakeVariant(""), nil
}

func (m *menuObject) Event(id int32, eventID string, data dbus.Variant, timestamp uint32) *dbus.Error {
	if eventID == "clicked" && id >= 1 && int(id) <= len(m.items) {
		if f := m.items[id-1].OnClick; f != nil {
			go f()
		}
	}
	return nil
}

func (m *menuObject) EventGroup(events []menuEvent) ([]int32, *dbus.Error) {
	for _, e := range events {
		_ = m.Event(e.ID, e.EventID, e.Data, e.Timestamp)
	}
	return []int32{}, nil
}

func (m *menuObject) AboutToShow(id int32) (bool, *dbus.Error) { return false, nil }

func (m *menuObject) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return []int32{}, []int32{}, nil
}
