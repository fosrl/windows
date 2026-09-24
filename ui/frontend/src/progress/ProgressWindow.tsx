import { useEffect, useState } from "react";
import { AppService } from "@bindings";
import { Marquee } from "../components/controls";
import { report, useEvent } from "../lib";

/** Small window with a status line and a marquee bar, used for updates and the CLI install. */
export function ProgressWindow({ kind }: { kind: string }) {
  const [text, setText] = useState("");

  useEffect(() => {
    report(AppService.ProgressText(kind).then(setText));
  }, [kind]);
  useEvent<string>("progress:text", (t) => {
    if (kind === "update") setText(t);
  });

  return (
    <div className="flex h-full flex-col justify-center gap-3 px-5 py-4">
      <div>{text}</div>
      <Marquee />
    </div>
  );
}
