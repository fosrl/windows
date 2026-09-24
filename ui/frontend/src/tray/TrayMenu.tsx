import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { MenuService, type MenuItem, type MenuState, type TrayLayout } from "@bindings";
import { report, useEvent } from "../lib";

const defaultLayout: TrayLayout = { submenuSide: "left", anchor: "bottom", panelWidth: 260, padding: 8 };
// Matches the delay Windows uses before opening a submenu on hover.
const submenuHoverDelay = 350;

/**
 * The tray popup. The window is transparent and sized for the menu panel plus
 * one cascading submenu; clicking the empty area dismisses it like a menu.
 */
export function TrayMenu() {
  const [state, setState] = useState<MenuState>({ items: [] });
  const [layout, setLayout] = useState<TrayLayout>(defaultLayout);
  const [openSub, setOpenSub] = useState<{ id: string; top: number } | null>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const hoverTimer = useRef<number | undefined>(undefined);
  const measure = useRef<() => void>(() => {});

  useEffect(() => {
    report(MenuService.State().then(setState));
    report(MenuService.Layout().then(setLayout));
  }, []);
  useEvent<MenuState>("menu:state", setState);
  useEvent<TrayLayout>("tray:layout", setLayout);
  useEvent<TrayLayout>("tray:open", (l) => {
    setLayout(l);
    setOpenSub(null);
    // Layout is paused while the window is hidden, so heights reported then
    // can be stale. Measure again once the popup is on screen.
    requestAnimationFrame(() => requestAnimationFrame(() => measure.current()));
  });

  // Keep the native window as tall as the menu panel.
  useLayoutEffect(() => {
    const panel = panelRef.current;
    if (!panel) return;
    const send = () => {
      const height = Math.ceil(panel.getBoundingClientRect().height) + 2 * layout.padding;
      if (height > 2 * layout.padding) report(MenuService.Resize(height));
    };
    measure.current = send;
    send();
    const ro = new ResizeObserver(send);
    ro.observe(panel);
    // A viewport shorter than the menu means the window was sized from a stale
    // height; report again so the backend corrects it.
    window.addEventListener("resize", send);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", send);
    };
  }, [layout.padding]);

  const invoke = useCallback((item: MenuItem) => {
    if (!item.enabled || !item.id || item.kind !== "item") return;
    setOpenSub(null);
    report(MenuService.Invoke(item.id));
  }, []);

  const items = state.items ?? [];
  const sub = openSub ? items.find((i) => i.id === openSub.id) : undefined;

  const openSubmenu = (item: MenuItem, el: HTMLElement, immediate: boolean) => {
    window.clearTimeout(hoverTimer.current);
    const open = () => setOpenSub({ id: item.id, top: el.getBoundingClientRect().top });
    if (immediate) open();
    else hoverTimer.current = window.setTimeout(open, submenuHoverDelay);
  };
  const hoverPlainItem = () => {
    window.clearTimeout(hoverTimer.current);
    hoverTimer.current = window.setTimeout(() => setOpenSub(null), submenuHoverDelay);
  };

  const panelSide = layout.submenuSide === "left" ? { right: layout.padding } : { left: layout.padding };
  const panelEdge = layout.anchor === "bottom" ? { bottom: layout.padding } : { top: layout.padding };

  return (
    <div
      className="relative h-full w-full"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) report(MenuService.Hide());
      }}
      onContextMenu={(e) => e.preventDefault()}
    >
      <div
        ref={panelRef}
        className="absolute"
        style={{ ...panelSide, ...panelEdge, width: layout.panelWidth }}
      >
        <MenuPanel
          items={items}
          activeSubmenu={openSub?.id}
          onInvoke={invoke}
          onHoverSubmenu={(item, el) => openSubmenu(item, el, false)}
          onClickSubmenu={(item, el) => openSubmenu(item, el, true)}
          onHoverItem={hoverPlainItem}
        />
      </div>
      {sub?.items && openSub && (
        <Submenu
          items={sub.items}
          anchorTop={openSub.top}
          layout={layout}
          onInvoke={invoke}
          onEnter={() => window.clearTimeout(hoverTimer.current)}
        />
      )}
    </div>
  );
}

function Submenu({
  items,
  anchorTop,
  layout,
  onInvoke,
  onEnter,
}: {
  items: MenuItem[];
  anchorTop: number;
  layout: TrayLayout;
  onInvoke: (item: MenuItem) => void;
  onEnter: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [top, setTop] = useState(anchorTop - 4);

  // Align with the parent item, then shift up so the submenu stays inside the window.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const maxBottom = window.innerHeight - layout.padding;
    let t = anchorTop - 4;
    if (t + el.offsetHeight > maxBottom) t = maxBottom - el.offsetHeight;
    setTop(Math.max(layout.padding, t));
  }, [anchorTop, items, layout.padding]);

  const side =
    layout.submenuSide === "left"
      ? { right: layout.padding + layout.panelWidth - 2 }
      : { left: layout.padding + layout.panelWidth - 2 };

  return (
    <div
      ref={ref}
      className="absolute overflow-y-auto"
      style={{ ...side, top, width: layout.panelWidth, maxHeight: `calc(100% - ${2 * layout.padding}px)` }}
      onMouseEnter={onEnter}
    >
      <MenuPanel items={items} onInvoke={onInvoke} />
    </div>
  );
}

function MenuPanel({
  items,
  activeSubmenu,
  onInvoke,
  onHoverSubmenu,
  onClickSubmenu,
  onHoverItem,
}: {
  items: MenuItem[];
  activeSubmenu?: string;
  onInvoke: (item: MenuItem) => void;
  onHoverSubmenu?: (item: MenuItem, el: HTMLElement) => void;
  onClickSubmenu?: (item: MenuItem, el: HTMLElement) => void;
  onHoverItem?: () => void;
}) {
  const checkColumn = items.some((i) => i.checked);
  return (
    <div
      role="menu"
      className="rounded-[8px] border border-menu-border bg-menu p-1 shadow-[0_4px_12px_rgba(0,0,0,0.18)]"
    >
      {items.map((item, i) => {
        if (item.kind === "separator") {
          return <div key={`sep-${i}`} role="separator" className="mx-1 my-1 h-px bg-menu-border" />;
        }
        const isSubmenu = item.kind === "submenu";
        const disabled = !item.enabled || item.kind === "header";
        return (
          <div
            key={item.id || `${item.label}-${i}`}
            role={isSubmenu ? "menuitem" : item.checked ? "menuitemcheckbox" : "menuitem"}
            aria-checked={item.checked || undefined}
            aria-haspopup={isSubmenu || undefined}
            aria-disabled={disabled || undefined}
            onMouseEnter={(e) => {
              if (isSubmenu) onHoverSubmenu?.(item, e.currentTarget);
              else onHoverItem?.();
            }}
            onClick={(e) => (isSubmenu ? onClickSubmenu?.(item, e.currentTarget) : onInvoke(item))}
            className={[
              "flex min-h-[28px] items-center gap-2 rounded-[4px] px-2 py-1",
              disabled ? "text-text-disabled" : "cursor-default hover:bg-menu-hover",
              isSubmenu && activeSubmenu === item.id ? "bg-menu-hover" : "",
            ].join(" ")}
          >
            {checkColumn && (
              <span className="w-4 shrink-0 text-center text-[11px]">{item.checked ? "✓" : ""}</span>
            )}
            <span className="min-w-0 flex-1 break-words">{item.label}</span>
            {isSubmenu && <span className="shrink-0 text-[10px] text-text-secondary">❯</span>}
          </div>
        );
      })}
    </div>
  );
}
