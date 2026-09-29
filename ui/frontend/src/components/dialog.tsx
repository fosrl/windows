import { useEffect, type ReactNode } from "react";
import appIcon from "../assets/app_icon.svg";

/**
 * The layout of a small single-task window, such as installing an update: the
 * app icon beside a bold title and body, with buttons along the bottom right.
 * Escape runs `onEscape`.
 */
export function DialogWindow({
  title,
  children,
  buttons,
  onEscape,
}: {
  title: string;
  children?: ReactNode;
  buttons: ReactNode;
  onEscape: () => void;
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onEscape();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onEscape]);

  return (
    <div className="flex h-full flex-col px-5 pt-5 pb-4 select-none">
      <div className="flex gap-4">
        <img src={appIcon} alt="" className="size-[56px] shrink-0" />
        <div className="min-w-0 flex-1">
          <div className="text-[13px] font-bold break-words">{title}</div>
          {children}
        </div>
      </div>
      <div className="mt-auto flex justify-end gap-2 pt-4">{buttons}</div>
    </div>
  );
}

/** Secondary text under a dialog window's title. */
export function DialogMessage({ children }: { children: ReactNode }) {
  return <div className="mt-1 text-[12px] leading-[16px] break-words text-mac-secondary">{children}</div>;
}
