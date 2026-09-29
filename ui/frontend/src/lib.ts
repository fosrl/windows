import { useEffect, useRef } from "react";
import { Events } from "@wailsio/runtime";

/** Subscribes to a backend event for the lifetime of the component. */
export function useEvent<T>(name: string, handler: (data: T) => void) {
  const ref = useRef(handler);
  ref.current = handler;
  useEffect(() => Events.On(name, (ev) => ref.current(ev.data as T)), [name]);
}

/** Logs a failed backend call; calls never throw into the UI. */
export function report(p: Promise<unknown>) {
  p.catch((err) => console.error(err));
}

export const urls = {
  docs: "https://docs.pangolin.net/",
  howItWorks: "https://docs.pangolin.net/about/how-pangolin-works",
  terms: "https://pangolin.net/tos",
  privacy: "https://pangolin.net/privacy",
  configureClient: "https://docs.pangolin.net/manage/clients/configure-client",
};
