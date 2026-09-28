import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { LogsService, type LogEntry, type LogsSnapshot } from "@bindings";
import { Button } from "../components/controls";
import { report, useEvent } from "../lib";

const rowHeight = 22;
// Auto-scroll when the user is within this many rows of the bottom.
const autoScrollThreshold = 10;
// Fixed width of the Time and Level columns plus the Message cell padding.
const fixedColumnsWidth = 180 + 70 + 16;

export function LogsTab({ visible }: { visible: boolean }) {
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const anchor = useRef<number | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const headerRef = useRef<HTMLDivElement>(null);
  const stickToBottom = useRef(true);

  const virtualizer = useVirtualizer({
    count: entries.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan: 20,
  });

  // Rows are virtualized, so size the content to the longest line up front
  // (monospace, so characters map to `ch`) to give it a stable scroll width.
  const longestLine = useMemo(() => entries.reduce((max, e) => Math.max(max, e.line.length), 0), [entries]);
  const contentWidth = `calc(${fixedColumnsWidth}px + ${longestLine}ch)`;

  const nearBottom = () => {
    const el = scrollRef.current;
    if (!el) return true;
    return el.scrollHeight - el.scrollTop - el.clientHeight <= autoScrollThreshold * rowHeight;
  };

  const replace = useCallback((snap: LogsSnapshot) => {
    stickToBottom.current = true;
    setEntries(snap.entries ?? []);
    setSelected(new Set());
    anchor.current = null;
  }, []);

  useEffect(() => {
    report(LogsService.Snapshot().then(replace));
  }, [replace]);
  useEvent<LogsSnapshot>("logs:reset", replace);
  useEvent<LogsSnapshot>("logs:append", (snap) => {
    const added = snap.entries ?? [];
    if (added.length === 0) return;
    stickToBottom.current = nearBottom() && selected.size <= 1;
    setEntries((prev) => {
      const last = prev.length ? prev[prev.length - 1].seq : 0;
      const next = prev.concat(added.filter((e) => e.seq > last));
      return next.length > 10000 ? next.slice(next.length - 10000) : next;
    });
  });

  useLayoutEffect(() => {
    if (stickToBottom.current && entries.length > 0) {
      virtualizer.scrollToIndex(entries.length - 1, { align: "end" });
    }
  }, [entries, virtualizer]);

  // Scroll to the newest line when the tab is first shown.
  useLayoutEffect(() => {
    if (visible && stickToBottom.current && entries.length > 0) {
      virtualizer.scrollToIndex(entries.length - 1, { align: "end" });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible]);

  const selectedSeqs = () => entries.filter((e) => selected.has(e.seq)).map((e) => e.seq);
  const copy = () => {
    const seqs = selectedSeqs();
    if (seqs.length) report(LogsService.Copy(seqs));
  };
  const selectAll = () => setSelected(new Set(entries.map((e) => e.seq)));
  const save = () => report(LogsService.Export());
  const clear = () => report(LogsService.Clear());

  const clickRow = (index: number, e: React.MouseEvent) => {
    const seq = entries[index].seq;
    if (e.shiftKey && anchor.current !== null) {
      const a = entries.findIndex((x) => x.seq === anchor.current);
      const [lo, hi] = a < index ? [a, index] : [index, a];
      const range = new Set(e.ctrlKey ? selected : []);
      for (let i = Math.max(lo, 0); i <= hi; i++) range.add(entries[i].seq);
      setSelected(range);
      return;
    }
    if (e.ctrlKey) {
      const next = new Set(selected);
      if (next.has(seq)) next.delete(seq);
      else next.add(seq);
      setSelected(next);
    } else {
      setSelected(new Set([seq]));
    }
    anchor.current = seq;
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (!e.ctrlKey) return;
    const key = e.key.toLowerCase();
    if (key === "c") copy();
    else if (key === "a") selectAll();
    else if (key === "s") save();
    else return;
    e.preventDefault();
  };

  return (
    <div
      className="flex min-h-0 min-w-0 flex-1 flex-col gap-3 px-5 pt-3 pb-4"
      onKeyDown={onKeyDown}
      onClick={() => setMenu(null)}
    >
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-[10px] border border-mac-group-border bg-mac-group">
        <div ref={headerRef} className="shrink-0 overflow-hidden border-b border-mac-separator">
          <div className="font-mono text-[11.5px]" style={{ width: contentWidth, minWidth: "100%" }}>
            <div className="flex h-[26px] font-sans text-[12px] font-semibold text-mac-secondary">
              <div className="w-[180px] shrink-0 px-2.5 leading-[26px]">Time</div>
              <div className="w-[70px] shrink-0 border-l border-mac-separator px-2 leading-[26px]">Level</div>
              <div className="min-w-0 flex-1 border-l border-mac-separator px-2 leading-[26px]">Message</div>
            </div>
          </div>
        </div>
        <div
          ref={scrollRef}
          tabIndex={0}
          className="min-h-0 flex-1 overflow-auto outline-none"
          onScroll={(e) => {
            if (headerRef.current) headerRef.current.scrollLeft = e.currentTarget.scrollLeft;
          }}
          onContextMenu={(e) => {
            e.preventDefault();
            setMenu({ x: e.clientX, y: e.clientY });
          }}
        >
          {entries.length === 0 && (
            <div className="flex h-full items-center justify-center text-mac-secondary">No log messages</div>
          )}
          <div
            className="relative font-mono text-[11.5px]"
            style={{ height: virtualizer.getTotalSize(), width: contentWidth, minWidth: "100%" }}
          >
            {virtualizer.getVirtualItems().map((row) => {
              const entry = entries[row.index];
              const isSelected = selected.has(entry.seq);
              return (
                <div
                  key={entry.seq}
                  onMouseDown={(e) => {
                    if (e.button === 0 || !isSelected) clickRow(row.index, e);
                  }}
                  className={[
                    "absolute left-0 flex w-full font-mono text-[11.5px]",
                    isSelected ? "bg-mac-accent text-white" : row.index % 2 ? "bg-mac-row-alt" : "",
                  ].join(" ")}
                  style={{ top: row.start, height: rowHeight, lineHeight: `${rowHeight}px` }}
                >
                  <div className={isSelected ? "w-[180px] shrink-0 truncate px-2.5" : "w-[180px] shrink-0 truncate px-2.5 text-mac-secondary"}>
                    {entry.stamp}
                  </div>
                  <div className={["w-[70px] shrink-0 truncate px-2", isSelected ? "" : levelColor(entry.level)].join(" ")}>
                    {entry.level}
                  </div>
                  <div className="min-w-0 flex-1 whitespace-pre px-2">
                    {entry.line}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>

      <div className="flex shrink-0 items-center gap-2">
        <span className="flex-1 text-[11px] text-mac-secondary">
          {entries.length.toLocaleString()} {entries.length === 1 ? "message" : "messages"}
        </span>
        <Button onClick={clear}>Clear</Button>
        <Button onClick={save}>Save...</Button>
      </div>

      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          items={[
            { label: "Copy", shortcut: "Ctrl+C", enabled: selected.size > 0, run: copy },
            { label: "Select All", shortcut: "Ctrl+A", enabled: selected.size < entries.length, run: selectAll },
            { label: "Save to File...", shortcut: "Ctrl+S", enabled: true, run: save },
          ]}
        />
      )}
    </div>
  );
}

function levelColor(level: string) {
  switch (level.toUpperCase()) {
    case "ERROR":
    case "FATAL":
      return "text-mac-danger";
    case "WARN":
    case "WARNING":
      return "text-[#c27c00] dark:text-[#ffb340]";
    default:
      return "text-mac-secondary";
  }
}

function ContextMenu({
  x,
  y,
  items,
  onClose,
}: {
  x: number;
  y: number;
  items: { label: string; shortcut: string; enabled: boolean; run: () => void }[];
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ x, y });

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    setPos({
      x: Math.min(x, window.innerWidth - el.offsetWidth - 4),
      y: Math.min(y, window.innerHeight - el.offsetHeight - 4),
    });
  }, [x, y]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    window.addEventListener("blur", onClose);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("blur", onClose);
    };
  }, [onClose]);

  return (
    <div
      ref={ref}
      role="menu"
      className="fixed z-50 min-w-[190px] rounded-[8px] border border-mac-group-border bg-mac-sheet p-[5px] shadow-[0_8px_24px_rgb(0_0_0/0.22)]"
      style={{ left: pos.x, top: pos.y }}
      onMouseDown={(e) => e.stopPropagation()}
    >
      {items.map((it) => (
        <div
          key={it.label}
          role="menuitem"
          aria-disabled={!it.enabled || undefined}
          onClick={(e) => {
            e.stopPropagation();
            if (!it.enabled) return;
            onClose();
            it.run();
          }}
          className={[
            "flex h-[22px] items-center gap-6 rounded-[4px] px-2 text-[13px]",
            it.enabled ? "cursor-default hover:bg-mac-accent hover:text-white" : "text-mac-tertiary",
          ].join(" ")}
        >
          <span className="flex-1">{it.label}</span>
          <span className="opacity-60">{it.shortcut}</span>
        </div>
      ))}
    </div>
  );
}
