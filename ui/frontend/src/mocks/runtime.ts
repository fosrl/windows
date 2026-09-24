// Stand-in for @wailsio/runtime when previewing the UI in a plain browser (`npm run mock`).
type Handler = (ev: { data: unknown }) => void;
const handlers = new Map<string, Set<Handler>>();

export const Events = {
  On(name: string, cb: Handler) {
    if (!handlers.has(name)) handlers.set(name, new Set());
    handlers.get(name)!.add(cb);
    return () => handlers.get(name)!.delete(cb);
  },
  Emit(name: string, data: unknown) {
    handlers.get(name)?.forEach((cb) => cb({ data }));
  },
};
