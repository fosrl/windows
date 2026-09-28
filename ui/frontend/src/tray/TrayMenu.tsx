import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode, type Ref } from "react";
import { MenuService, type MenuItem, type MenuState, type TrayLayout } from "@bindings";
import { report, useEvent } from "../lib";
import { Chevron, Spinner, StatusDot, Switch, cx } from "../components/controls";

// Geometry and timing follow the macOS menu bar panel (MenuBarComponents.swift
// and MenuSubmenuPanel.swift).
const defaultLayout: TrayLayout = { submenuSide: "left", anchor: "bottom", panelWidth: 310, padding: 8 };
const submenuHoverDelay = 120;
const submenuMinWidth = 180;
// The loading spinner stays up at least this long so it doesn't flash.
const minLoadingMs = 700;
// Spinner height before the menu content has been measured.
const defaultLoadingHeight = 180;
// Border and padding around a panel's rows.
const panelChrome = 14;

// The panel eases to its new height like the macOS menu's `.smooth(duration: 0.35)`.
const heightMs = 350;
const heightEasing = "cubic-bezier(0.32, 0.72, 0, 1)";
const openMs = 180;
const openEasing = "cubic-bezier(0.2, 0.8, 0.2, 1)";
// How long a grow waits for the native window to catch up before animating.
const viewportWaitMs = 150;

// Until the first open the panel is transparent, except in the browser preview.
const startsHidden = import.meta.env.MODE !== "mock";

const reducedMotion = () => window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;

type OpenSubmenu = { id: string; anchor: HTMLElement };

const opensSubmenu = (item: MenuItem) =>
  item.kind === "submenu" || (item.kind === "status" && !!item.items?.length);

/** Resolves once the viewport is at least `height` tall, or after a short wait. */
function waitForViewport(height: number) {
  return new Promise<void>((resolve) => {
    const start = performance.now();
    const check = () => {
      if (window.innerHeight >= height || performance.now() - start > viewportWaitMs) resolve();
      else requestAnimationFrame(check);
    };
    check();
  });
}

/**
 * The tray popup. The window is transparent and sized for the menu panel plus
 * two cascading submenus; clicking the empty area dismisses it like a menu.
 */
export function TrayMenu() {
  const [state, setState] = useState<MenuState>({ items: [], loading: false });
  const [layout, setLayout] = useState<TrayLayout>(defaultLayout);
  // The open submenus, outermost first.
  const [path, setPath] = useState<OpenSubmenu[]>([]);
  const [firstSubmenuWidth, setFirstSubmenuWidth] = useState(0);
  const panelRef = useRef<HTMLDivElement>(null);
  const hoverTimer = useRef<number | undefined>(undefined);
  const loading = useMinimumDuration(state.loading, minLoadingMs);
  // Only a rendered menu's height is kept for the spinner, not the empty
  // panel shown before the first state arrives.
  const measuresContent = useRef(false);
  measuresContent.current = !loading && (state.items?.length ?? 0) > 0;
  const contentHeight = useRef(defaultLoadingHeight);
  const { boxRef, contentRef, height, animated, remeasure, setAnimated } = useAnimatedHeight(layout.padding);

  useEffect(() => {
    report(MenuService.State().then(setState));
    report(MenuService.Layout().then(setLayout));
  }, []);
  useEvent<MenuState>("menu:state", setState);
  useEvent<TrayLayout>("tray:layout", setLayout);

  // Opening: the backend shows the window off screen, the popup draws the
  // current menu while still transparent and reports its height, and the
  // backend then moves the window into place ("tray:shown") and the panel
  // slides in. Hiding: the popup paints itself transparent before the window
  // hides, so nothing stale is left to flash on the next show.
  const clearPanel = useCallback(() => {
    const el = panelRef.current;
    if (!el) return;
    el.getAnimations().forEach((a) => a.cancel());
    el.style.opacity = "0";
  }, []);
  useEvent<TrayLayout>("tray:open", (l) => {
    clearPanel();
    setLayout(l);
    setPath([]);
    setAnimated(false);
    // Let the new layout and state render, then size the panel and report.
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        const needed = remeasure();
        report(MenuService.OpenReady(needed ?? 0));
      }),
    );
  });
  useEvent<void>("tray:shown", () => {
    requestAnimationFrame(() => {
      const el = panelRef.current;
      if (!el) return;
      el.style.opacity = "";
      if (!reducedMotion()) {
        const from = layout.anchor === "bottom" ? 8 : -8;
        el.animate(
          [
            { opacity: 0, transform: `translateY(${from}px)` },
            { opacity: 1, transform: "none" },
          ],
          { duration: openMs, easing: openEasing },
        );
      }
      // Later size changes animate.
      requestAnimationFrame(() => setAnimated(true));
    });
  });
  useEvent<void>("tray:hide", () => {
    clearPanel();
    setAnimated(false);
    setPath([]);
    // Two frames so the transparent frame is on screen before the window hides.
    requestAnimationFrame(() => requestAnimationFrame(() => report(MenuService.HideReady())));
  });

  useLayoutEffect(() => {
    if (measuresContent.current && height) contentHeight.current = height;
  });

  const items = state.items ?? [];
  const first = path[0] && !loading ? items.find((i) => i.id === path[0].id && opensSubmenu(i)) : undefined;
  const second = first && path[1] ? first.items?.find((i) => i.id === path[1].id && opensSubmenu(i)) : undefined;

  // Close submenus whose rows went away, e.g. Sites once the tunnel disconnects.
  useEffect(() => {
    if (path.length > 0 && !first) setPath([]);
    else if (path.length > 1 && !second) setPath((p) => p.slice(0, 1));
  }, [path, first, second]);

  // Site status is only polled while the sites submenu is on screen.
  const sitesOpen = first?.id === "sites";
  useEffect(() => {
    report(MenuService.SetSitesVisible(sitesOpen));
  }, [sitesOpen]);

  const schedule = (fn: () => void, immediate: boolean) => {
    window.clearTimeout(hoverTimer.current);
    if (immediate) fn();
    else hoverTimer.current = window.setTimeout(fn, submenuHoverDelay);
  };
  const openSubmenu = (level: number, item: MenuItem, el: HTMLElement, immediate: boolean) =>
    schedule(() => {
      setPath((p) =>
        p[level]?.id === item.id && p[level].anchor === el ? p : [...p.slice(0, level), { id: item.id, anchor: el }],
      );
    }, immediate);
  // Moving onto any other row closes the submenu open beside it.
  const closeSubmenus = (level: number) =>
    schedule(() => setPath((p) => (p.length > level ? p.slice(0, level) : p)), false);
  const cancelPending = () => window.clearTimeout(hoverTimer.current);

  const invoke = useCallback((item: MenuItem) => {
    if (!item.enabled || !item.id) return;
    if (item.kind !== "item" && item.kind !== "toggle") return;
    setPath([]);
    report(MenuService.Invoke(item.id));
  }, []);

  const panelSide = layout.submenuSide === "left" ? { right: layout.padding } : { left: layout.padding };
  const panelEdge = layout.anchor === "bottom" ? { bottom: layout.padding } : { top: layout.padding };
  const firstOffset = layout.padding + layout.panelWidth - 2;
  const secondOffset = firstOffset + firstSubmenuWidth - 2;

  return (
    <div
      className="relative h-full w-full text-[13px] text-mac-label"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) report(MenuService.Hide());
      }}
      onContextMenu={(e) => e.preventDefault()}
    >
      <div
        ref={panelRef}
        className="absolute"
        style={{ ...panelSide, ...panelEdge, width: layout.panelWidth, opacity: startsHidden ? 0 : undefined }}
      >
        <Panel
          boxRef={boxRef}
          style={{
            height,
            overflow: "hidden",
            transition: animated ? `height ${heightMs}ms ${heightEasing}` : undefined,
          }}
        >
          <div ref={contentRef}>
            {/* Keyed so the spinner and the menu cross-fade. */}
            <div key={loading ? "loading" : "menu"} className="tray-fade-in">
              {loading ? (
                <div
                  className="flex items-center justify-center"
                  style={{ height: contentHeight.current - panelChrome }}
                >
                  <Spinner size={28} />
                </div>
              ) : (
                <MenuRows
                  items={items}
                  activeSubmenu={path[0]?.id}
                  onInvoke={invoke}
                  onOpenSubmenu={(item, el, immediate) => openSubmenu(0, item, el, immediate)}
                  onHoverRow={() => closeSubmenus(0)}
                />
              )}
            </div>
          </div>
        </Panel>
      </div>
      {first?.items && path[0] && (
        <Submenu
          key={path[0].id}
          items={first.items}
          anchor={path[0].anchor}
          offset={firstOffset}
          layout={layout}
          activeSubmenu={path[1]?.id}
          onInvoke={invoke}
          onEnter={cancelPending}
          onOpenSubmenu={(item, el, immediate) => openSubmenu(1, item, el, immediate)}
          onHoverRow={() => closeSubmenus(1)}
          onWidth={setFirstSubmenuWidth}
        />
      )}
      {second?.items && path[1] && (
        <Submenu
          key={path[1].id}
          items={second.items}
          anchor={path[1].anchor}
          offset={secondOffset}
          layout={layout}
          onInvoke={invoke}
          onEnter={cancelPending}
          onHoverRow={() => {}}
        />
      )}
    </div>
  );
}

/**
 * Animates the main panel's height to fit its content. The backend keeps the
 * window at least as tall as reported, with headroom, and never shrinks it
 * while open, so most changes animate without the window moving. When the
 * menu outgrows the window, the panel waits for the window to grow first.
 */
function useAnimatedHeight(padding: number) {
  const boxRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState<number | undefined>(undefined);
  // Off while the popup is hidden and while it opens, so it opens at its size.
  const [animated, setAnimatedState] = useState(true);
  const animatedRef = useRef(true);
  const target = useRef(0);

  const setAnimated = useCallback((on: boolean) => {
    animatedRef.current = on;
    setAnimatedState(on);
  }, []);

  const windowHeight = useCallback((h: number) => Math.ceil(h) + 2 * padding, [padding]);

  // Returns the window height the menu needs, when it has content.
  const fit = useCallback(
    (force: boolean): number | undefined => {
      const content = contentRef.current;
      if (!content) return undefined;
      const natural = content.offsetHeight + panelChrome;
      if (natural <= panelChrome) return undefined;
      if (!force && natural === target.current) return windowHeight(natural);
      target.current = natural;
      report(MenuService.Resize(windowHeight(natural)));

      if (force || !animatedRef.current || reducedMotion() || window.innerHeight >= windowHeight(natural)) {
        setHeight(natural);
        return windowHeight(natural);
      }
      // Grow into the window once it has caught up.
      void waitForViewport(windowHeight(natural)).then(() => {
        if (target.current === natural) setHeight(natural);
      });
      return windowHeight(natural);
    },
    [windowHeight],
  );

  useLayoutEffect(() => {
    const content = contentRef.current;
    if (!content) return;
    fit(false);
    const ro = new ResizeObserver(() => void fit(false));
    ro.observe(content);
    return () => ro.disconnect();
  }, [fit]);

  const remeasure = useCallback(() => fit(true), [fit]);
  return { boxRef, contentRef, height, animated, remeasure, setAnimated };
}

/** Returns true while `active` is, and for at least `ms` after it became true. */
function useMinimumDuration(active: boolean, ms: number) {
  const [shown, setShown] = useState(active);
  const since = useRef(active ? Date.now() : 0);
  useEffect(() => {
    if (active) {
      since.current = Date.now();
      setShown(true);
      return;
    }
    const wait = since.current + ms - Date.now();
    if (wait <= 0) {
      setShown(false);
      return;
    }
    const t = window.setTimeout(() => setShown(false), wait);
    return () => window.clearTimeout(t);
  }, [active, ms]);
  return shown;
}

function Submenu({
  items,
  anchor,
  offset,
  layout,
  activeSubmenu,
  onInvoke,
  onEnter,
  onOpenSubmenu,
  onHoverRow,
  onWidth,
}: {
  items: MenuItem[];
  anchor: HTMLElement;
  offset: number;
  layout: TrayLayout;
  activeSubmenu?: string;
  onInvoke: (item: MenuItem) => void;
  onEnter: () => void;
  onOpenSubmenu?: (item: MenuItem, el: HTMLElement, immediate: boolean) => void;
  onHoverRow: () => void;
  onWidth?: (width: number) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [top, setTop] = useState(() => anchor.getBoundingClientRect().top - 6);

  // Line the first row up with the parent row, and keep following it while
  // the panel above animates or the window moves. Shift up so the submenu
  // stays inside the window.
  useLayoutEffect(() => {
    let frame = 0;
    let last = NaN;
    const place = () => {
      const el = ref.current;
      if (el) {
        const maxBottom = window.innerHeight - layout.padding;
        let t = anchor.getBoundingClientRect().top - 6;
        if (t + el.offsetHeight > maxBottom) t = maxBottom - el.offsetHeight;
        t = Math.max(layout.padding, t);
        if (t !== last) {
          last = t;
          setTop(t);
        }
      }
      frame = requestAnimationFrame(place);
    };
    place();
    return () => cancelAnimationFrame(frame);
  }, [anchor, layout.padding]);

  useLayoutEffect(() => {
    if (ref.current) onWidth?.(ref.current.offsetWidth);
  }, [items, onWidth]);

  const side = layout.submenuSide === "left" ? { right: offset } : { left: offset };

  return (
    <div
      ref={ref}
      className={layout.submenuSide === "left" ? "tray-submenu-in-left absolute" : "tray-submenu-in-right absolute"}
      style={{ ...side, top, width: "max-content", minWidth: submenuMinWidth, maxWidth: layout.panelWidth }}
      onMouseEnter={onEnter}
    >
      <Panel maxHeight={`calc(100vh - ${2 * layout.padding}px)`}>
        <MenuRows
          items={items}
          activeSubmenu={activeSubmenu}
          onInvoke={onInvoke}
          onOpenSubmenu={onOpenSubmenu}
          onHoverRow={onHoverRow}
        />
      </Panel>
    </div>
  );
}

function Panel({
  children,
  maxHeight,
  style,
  boxRef,
}: {
  children: ReactNode;
  maxHeight?: string;
  style?: CSSProperties;
  boxRef?: Ref<HTMLDivElement>;
}) {
  return (
    <div
      ref={boxRef}
      role="menu"
      className="overflow-y-auto rounded-[10px] border border-mac-menu-border bg-mac-menu p-[6px] shadow-[0_2px_8px_rgba(0,0,0,0.2)]"
      style={{ maxHeight, ...style }}
    >
      {children}
    </div>
  );
}

function MenuRows({
  items,
  activeSubmenu,
  onInvoke,
  onOpenSubmenu,
  onHoverRow,
}: {
  items: MenuItem[];
  activeSubmenu?: string;
  onInvoke: (item: MenuItem) => void;
  onOpenSubmenu?: (item: MenuItem, el: HTMLElement, immediate: boolean) => void;
  onHoverRow: () => void;
}) {
  // Checkable rows reserve a check column so checked and unchecked rows line up.
  const checkColumn = items.some((i) => i.checkable);
  return (
    <>
      {items.map((item, i) => (
        <Row
          key={item.id || `${item.kind}-${item.label}-${i}`}
          item={item}
          checkColumn={checkColumn}
          open={activeSubmenu !== undefined && activeSubmenu === item.id}
          onInvoke={onInvoke}
          onOpenSubmenu={onOpenSubmenu}
          onHoverRow={onHoverRow}
        />
      ))}
    </>
  );
}

const rowPadding = "px-[10px]";
const highlight =
  "rounded-[6px] transition-colors duration-100 hover:bg-mac-menu-hover hover:duration-0 active:bg-mac-menu-pressed";

function Row({
  item,
  checkColumn,
  open,
  onInvoke,
  onOpenSubmenu,
  onHoverRow,
}: {
  item: MenuItem;
  checkColumn: boolean;
  open: boolean;
  onInvoke: (item: MenuItem) => void;
  onOpenSubmenu?: (item: MenuItem, el: HTMLElement, immediate: boolean) => void;
  onHoverRow: () => void;
}) {
  switch (item.kind) {
    case "separator":
      return <div role="separator" className="mx-[10px] my-[6px] h-px bg-mac-separator" />;

    case "header":
      return (
        <div
          onMouseEnter={onHoverRow}
          className={cx(
            "flex min-h-[26px] items-center pt-1 pr-[10px] font-semibold text-mac-secondary",
            item.inset ? "pl-[29px]" : "pl-[10px]",
          )}
        >
          <span className="min-w-0 truncate">{item.label}</span>
        </div>
      );

    case "label":
      return (
        <div
          onMouseEnter={onHoverRow}
          className={cx("flex min-h-[26px] items-baseline gap-[6px] py-1 text-mac-secondary", rowPadding)}
        >
          {item.icon && <LabelIcon icon={item.icon} />}
          <span className="min-w-0 break-words">{item.label}</span>
        </div>
      );

    case "detail":
      return (
        <div
          onMouseEnter={onHoverRow}
          className={cx("flex min-h-[26px] items-baseline justify-between gap-4 py-1", rowPadding)}
        >
          <span className="shrink-0">{item.label}</span>
          <span className="flex min-w-0 items-center gap-[6px] text-right text-mac-secondary">
            {item.dot && <StatusDot color={item.dot} />}
            <span className="line-clamp-2 break-all">{item.value}</span>
          </span>
        </div>
      );

    case "toggle":
      return (
        <div
          role="menuitemcheckbox"
          aria-checked={item.checked}
          aria-disabled={!item.enabled || undefined}
          onMouseEnter={onHoverRow}
          onClick={() => onInvoke(item)}
          className={cx("flex min-h-[34px] items-center gap-2", rowPadding, item.enabled && highlight)}
        >
          <span className={cx("min-w-0 flex-1 truncate font-semibold", !item.enabled && "opacity-40")}>
            {item.label}
          </span>
          {/* The whole row flips the switch. */}
          <span className="pointer-events-none">
            <Switch checked={item.checked} onChange={() => {}} disabled={!item.enabled} label={item.label} />
          </span>
        </div>
      );

    case "status":
      if (!opensSubmenu(item)) {
        return (
          <div onMouseEnter={onHoverRow} className={cx("flex min-h-[26px] items-center", rowPadding)}>
            <StatusLabel item={item} />
          </div>
        );
      }
      break;
  }

  const isSubmenu = opensSubmenu(item);
  return (
    <div
      role="menuitem"
      aria-haspopup={isSubmenu || undefined}
      aria-checked={item.checkable ? item.checked : undefined}
      aria-disabled={!item.enabled || undefined}
      onMouseEnter={(e) => (isSubmenu ? onOpenSubmenu?.(item, e.currentTarget, false) : onHoverRow())}
      onClick={(e) => (isSubmenu ? onOpenSubmenu?.(item, e.currentTarget, true) : onInvoke(item))}
      className={cx(
        "flex min-h-[26px] items-center gap-[5px] whitespace-nowrap",
        rowPadding,
        item.enabled ? highlight : "opacity-40",
        open && "bg-mac-menu-hover",
      )}
    >
      {checkColumn && (
        <span className="flex w-[14px] shrink-0 items-center justify-center">
          {item.checkable && item.loading ? <Spinner size={11} /> : item.checked ? <Checkmark /> : null}
        </span>
      )}
      {item.kind === "status" ? (
        <StatusLabel item={item} />
      ) : (
        <span className="flex min-w-0 items-center gap-2">
          {item.dot && (
            <span className="flex size-[12px] shrink-0 items-center justify-center">
              <StatusDot color={item.dot} />
            </span>
          )}
          <span className="min-w-0 truncate">{item.label}</span>
        </span>
      )}
      <span className="min-w-[12px] flex-1" />
      {!item.checkable && item.loading && item.kind !== "status" && <Spinner size={11} />}
      {isSubmenu && <Chevron />}
    </div>
  );
}

/** The tunnel status line: dot, secondary text and a spinner while transitioning. */
function StatusLabel({ item }: { item: MenuItem }) {
  return (
    <span className="flex min-w-0 items-center gap-2">
      <span className="flex size-[12px] shrink-0 items-center justify-center">
        <StatusDot color={item.dot} />
      </span>
      <span key={item.label} className="tray-fade-in min-w-0 truncate text-mac-secondary">
        {item.label}
      </span>
      {item.loading && (
        <span className="tray-fade-in flex">
          <Spinner size={12} />
        </span>
      )}
    </span>
  );
}

function Checkmark() {
  return (
    <svg viewBox="0 0 12 12" className="size-[11px]" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
      <path d="M2.2 6.4l2.5 2.5 5.1-5.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function LabelIcon({ icon }: { icon: string }) {
  if (icon === "lock") {
    return (
      <svg viewBox="0 0 12 14" className="mt-[3px] h-[12px] w-[11px] shrink-0 self-start text-mac-secondary" aria-hidden>
        <path
          fill="currentColor"
          d="M6 1a3 3 0 0 0-3 3v2H2.5A1.5 1.5 0 0 0 1 7.5v4A1.5 1.5 0 0 0 2.5 13h7a1.5 1.5 0 0 0 1.5-1.5v-4A1.5 1.5 0 0 0 9.5 6H9V4a3 3 0 0 0-3-3zm-1.6 5V4a1.6 1.6 0 0 1 3.2 0v2z"
        />
      </svg>
    );
  }
  return (
    <svg viewBox="0 0 14 13" className="mt-[3px] h-[12px] w-[13px] shrink-0 self-start text-mac-warning" aria-hidden>
      <path
        fill="currentColor"
        d="M5.7 1.2a1.5 1.5 0 0 1 2.6 0l5 8.8A1.5 1.5 0 0 1 12 12.2H2A1.5 1.5 0 0 1 .7 10zM6.3 4.3v3.4h1.4V4.3zm0 4.6v1.4h1.4V8.9z"
      />
    </svg>
  );
}
