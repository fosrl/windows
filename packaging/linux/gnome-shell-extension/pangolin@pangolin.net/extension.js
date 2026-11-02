// The Pangolin client's top bar button for GNOME Shell.
//
// GNOME has no tray of its own, and the AppIndicator extension only opens an
// app's popup on a double click. This button talks to the running Pangolin
// client over D-Bus (net.pangolin.Client): a left click opens Pangolin's
// popup beside it, a right click offers Quit. While this extension runs, it
// owns net.pangolin.Client.ShellExtension so the client hides its regular
// tray icon. It also tells the client about clicks on the desktop
// background, which don't move keyboard focus and so wouldn't close the
// popup otherwise.

import Clutter from 'gi://Clutter';
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import St from 'gi://St';

import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

const CLIENT_NAME = 'net.pangolin.Client';
const CLIENT_PATH = '/net/pangolin/Client';
const EXTENSION_NAME = 'net.pangolin.Client.ShellExtension';
const CLIENT_WM_CLASS = 'pangolin-client';

const ClientProxy = Gio.DBusProxy.makeProxyWrapper(`
<node>
  <interface name="net.pangolin.Client1">
    <method name="Activate">
      <arg type="i" direction="in" name="x"/>
      <arg type="i" direction="in" name="y"/>
      <arg type="i" direction="in" name="stageWidth"/>
      <arg type="i" direction="in" name="stageHeight"/>
    </method>
    <method name="ClickAway"/>
    <method name="Quit"/>
    <property name="Icon" type="ay" access="read"/>
    <property name="Tooltip" type="s" access="read"/>
    <property name="PopupVisible" type="b" access="read"/>
  </interface>
</node>`);

export default class PangolinExtension extends Extension {
    enable() {
        this._ownId = Gio.bus_own_name(Gio.BusType.SESSION, EXTENSION_NAME,
            Gio.BusNameOwnerFlags.NONE, null, null, null);
        this._watchId = Gio.bus_watch_name(Gio.BusType.SESSION, CLIENT_NAME,
            Gio.BusNameWatcherFlags.NONE,
            () => this._clientAppeared(),
            () => this._clientVanished());
    }

    disable() {
        if (this._watchId) {
            Gio.bus_unwatch_name(this._watchId);
            this._watchId = 0;
        }
        if (this._ownId) {
            Gio.bus_unown_name(this._ownId);
            this._ownId = 0;
        }
        this._clientVanished();
    }

    _clientAppeared() {
        this._clientVanished();
        const cancellable = new Gio.Cancellable();
        this._cancellable = cancellable;
        // Constructing the proxy class starts an async init that calls back.
        new ClientProxy(Gio.DBus.session, CLIENT_NAME, CLIENT_PATH, (proxy, error) => {
            if (cancellable.is_cancelled())
                return;
            if (error) {
                console.error(`Pangolin: could not reach the client: ${error}`);
                return;
            }
            this._proxy = proxy;
            this._propsId = proxy.connect('g-properties-changed', () => this._sync());
            this._createButton();
            this._sync();
        }, cancellable);
    }

    _clientVanished() {
        this._cancellable?.cancel();
        this._cancellable = null;
        this._watchClicks(false);
        if (this._proxy && this._propsId)
            this._proxy.disconnect(this._propsId);
        this._proxy = null;
        this._propsId = 0;
        this._button?.destroy();
        this._button = null;
        this._icon = null;
    }

    _createButton() {
        const button = new PanelMenu.Button(0.0, 'Pangolin', false);
        this._icon = new St.Icon({style_class: 'system-status-icon'});
        button.add_child(this._icon);

        const quit = new PopupMenu.PopupMenuItem('Quit Pangolin');
        quit.connect('activate', () => this._proxy?.QuitAsync().catch(logError));
        button.menu.addMenuItem(quit);

        // A left click opens Pangolin's own popup; a right click opens the
        // Quit menu. GNOME 49 and later handle clicks with a ClickGesture,
        // which is replaced; older versions handle them in vfunc_event,
        // which the event signal runs ahead of.
        if (button._clickGesture) {
            button._clickGesture.set_enabled(false);
            const gesture = new Clutter.ClickGesture();
            gesture.set_recognize_on_press(true);
            gesture.connect('recognize', () => {
                if (gesture.get_button() === Clutter.BUTTON_SECONDARY)
                    button.menu.toggle();
                else
                    this._activate();
            });
            button.add_action(gesture);
        } else {
            button.connect('event', (_actor, event) => {
                const type = event.type();
                if (type === Clutter.EventType.TOUCH_BEGIN ||
                    (type === Clutter.EventType.BUTTON_PRESS && event.get_button() === Clutter.BUTTON_PRIMARY)) {
                    this._activate();
                    return Clutter.EVENT_STOP;
                }
                if (type === Clutter.EventType.BUTTON_RELEASE && event.get_button() === Clutter.BUTTON_PRIMARY)
                    return Clutter.EVENT_STOP;
                return Clutter.EVENT_PROPAGATE;
            });
        }

        this._button = button;
        Main.panel.addToStatusArea(this.uuid, button);
    }

    _activate() {
        if (!this._proxy || !this._button)
            return;
        // Anchor the popup under the middle of the button.
        const [bx, by] = this._button.get_transformed_position();
        const [bw, bh] = this._button.get_transformed_size();
        this._proxy.ActivateAsync(Math.round(bx + bw / 2), Math.round(by + bh),
            global.stage.width, global.stage.height).catch(logError);
    }

    _sync() {
        if (!this._proxy || !this._icon)
            return;
        const icon = this._proxy.get_cached_property('Icon');
        if (icon && icon.n_children() > 0)
            this._icon.gicon = Gio.BytesIcon.new(icon.get_data_as_bytes());
        const tooltip = this._proxy.get_cached_property('Tooltip')?.unpack() ?? '';
        this._button.accessible_name = tooltip || 'Pangolin';
        const visible = this._proxy.get_cached_property('PopupVisible')?.unpack() ?? false;
        this._watchClicks(visible);
    }

    // While the popup is open, a press anywhere outside Pangolin's windows and
    // outside this button closes it.
    _watchClicks(watch) {
        if (watch && !this._stageId) {
            this._stageId = global.stage.connect('captured-event', (_stage, event) => {
                const type = event.type();
                if (type === Clutter.EventType.BUTTON_PRESS || type === Clutter.EventType.TOUCH_BEGIN) {
                    const [x, y] = event.get_coords();
                    if (!this._insideClient(x, y) && !this._insideButton(x, y))
                        this._proxy?.ClickAwayAsync().catch(logError);
                }
                return Clutter.EVENT_PROPAGATE;
            });
        } else if (!watch && this._stageId) {
            global.stage.disconnect(this._stageId);
            this._stageId = 0;
        }
    }

    _insideButton(x, y) {
        if (!this._button)
            return false;
        const [bx, by] = this._button.get_transformed_position();
        const [bw, bh] = this._button.get_transformed_size();
        return x >= bx && x < bx + bw && y >= by && y < by + bh;
    }

    _insideClient(x, y) {
        return global.get_window_actors().some(actor => {
            const win = actor.meta_window;
            // X11 reports the class as "Pangolin-client" and the instance as
            // "pangolin-client".
            const wmClass = (win?.get_wm_class_instance?.() || win?.get_wm_class() || '').toLowerCase();
            if (!win || win.minimized || wmClass !== CLIENT_WM_CLASS)
                return false;
            const r = win.get_frame_rect();
            return x >= r.x && x < r.x + r.width && y >= r.y && y < r.y + r.height;
        });
    }
}

function logError(e) {
    if (!(e instanceof GLib.Error && e.matches(Gio.IOErrorEnum, Gio.IOErrorEnum.CANCELLED)))
        console.error(`Pangolin: ${e}`);
}
