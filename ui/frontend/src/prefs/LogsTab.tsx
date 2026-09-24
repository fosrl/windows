import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { LogsService, type LogEntry, type LogsSnapshot } from "@bindings";
import { Button } from "../components/controls";
import { report, useEvent } from "../lib";

const rowHeight = 20;
// Auto-scroll when the user is within this many rows of the bottom.
const autoScrollThreshold = 10;

export function LogsTab({ visible }: { visible: boolean }) {
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const anchor = useRef<number | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const stickToBottom = useRef(true);

  const virtualizer = useVirtualizer({
    count: entries.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan: 20,
  });

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
    <div className="flex min-h-0 min-w-0 flex-1 flex-col p-[9px]" onKeyDown={onKeyDown} onClick={() => setMenu(null)}>
      <div className="flex min-h-0 flex-1 flex-col border border-border-strong/60 bg-surface">
        <div className="flex h-[22px] shrink-0 border-b border-border text-left">
          <div className="w-[180px] shrink-0 border-r border-border px-1.5 leading-[22px]">Time</div>
          <div className="w-[80px] shrink-0 border-r border-border px-1.5 leading-[22px]">Level</div>
          <div className="min-w-0 flex-1 px-1.5 leading-[22px]">Log message</div>
        </div>
        <div
          ref={scrollRef}
          tabIndex={0}
          className="min-h-0 flex-1 overflow-auto outline-none"
          onContextMenu={(e) => {
            e.preventDefault();
            setMenu({ x: e.clientX, y: e.clientY });
          }}
        >
          <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
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
                    "absolute left-0 flex w-full border-b border-border/70",
                    isSelected ? "bg-row-selected" : row.index % 2 ? "bg-row-alt" : "",
                  ].join(" ")}
                  style={{ top: row.start, height: rowHeight }}
                >
                  <div className="w-[180px] shrink-0 truncate border-r border-border/70 px-1.5 leading-[19px]">
                    {entry.stamp}
                  </div>
                  <div className="w-[80px] shrink-0 truncate border-r border-border/70 px-1.5 leading-[19px]">
                    {entry.level}
                  </div>
                  <div className="min-w-0 flex-1 truncate px-1.5 leading-[19px]" title={entry.line}>
                    {entry.line}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>

      <div className="flex justify-end gap-2 pt-[9px]">
        <Button accessKey="c" onClick={clear}>
          Clear
        </Button>
        <Button accessKey="s" onClick={save}>
          Save
        </Button>
      </div>

      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          items={[
            { label: "Copy", shortcut: "Ctrl+C", enabled: selected.size > 0, run: copy },
            { label: "Select all", shortcut: "Ctrl+A", enabled: selected.size < entries.length, run: selectAll },
            { label: "Save to file…", shortcut: "Ctrl+S", enabled: true, run: save },
          ]}
        />
      )}
    </div>
  );
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
      className="fixed z-50 min-w-[180px] rounded-[8px] border border-menu-border bg-menu p-1 shadow-[0_4px_12px_rgba(0,0,0,0.18)]"
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
            "flex h-7 items-center gap-6 rounded-[4px] px-2",
            it.enabled ? "cursor-default hover:bg-menu-hover" : "text-text-disabled",
          ].join(" ")}
        >
          <span className="flex-1">{it.label}</span>
          <span className="text-text-secondary">{it.shortcut}</span>
        </div>
      ))}
    </div>
  );
}
