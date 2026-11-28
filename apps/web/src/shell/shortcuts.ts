import { useEffect, useRef } from "react";

// Ctrl+K / ⌘K opens the command palette, "?" opens the shortcut sheet, and
// "g" then a letter navigates (requirement 3.4). Single-key shortcuts are
// off while a text field has focus.

export const GO_KEYS = {
  o: "The Lab (overview)",
  f: "Lightning feed (activity)",
  a: "Account",
} as const;

export type GoKey = keyof typeof GO_KEYS;

export interface ShortcutHandlers {
  palette(): void;
  help(): void;
  go(key: GoKey): void;
}

const G_WINDOW_MS = 1200;

function isTyping(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName);
}

export function useShortcuts(handlers: ShortcutHandlers): void {
  const latest = useRef(handlers);
  latest.current = handlers;

  useEffect(() => {
    let gAt = 0;
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        latest.current.palette();
        return;
      }
      if (event.metaKey || event.ctrlKey || event.altKey || isTyping(event.target)) {
        return;
      }
      if (event.key === "?") {
        event.preventDefault();
        latest.current.help();
        return;
      }
      if (event.key === "g") {
        gAt = Date.now();
        return;
      }
      if (gAt && Date.now() - gAt < G_WINDOW_MS && event.key in GO_KEYS) {
        event.preventDefault();
        latest.current.go(event.key as GoKey);
      }
      gAt = 0;
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);
}
