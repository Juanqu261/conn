import { useSyncExternalStore } from "react";

// Per-browser conveniences only (theme, plain labels, last Project). Storage
// can be missing or throw in private windows, so every access is guarded
// and the Lab works without it.

export type ThemeChoice = "system" | "dark" | "light";

const KEYS = {
  theme: "conn-lab.theme",
  plain: "conn-lab.plain-labels",
  project: "conn-lab.last-project",
};

function read(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string | null): void {
  try {
    if (value === null) {
      window.localStorage.removeItem(key);
    } else {
      window.localStorage.setItem(key, value);
    }
  } catch {
    // Preferences are a convenience; losing one is harmless.
  }
}

const listeners = new Set<() => void>();
function emit(): void {
  for (const listener of listeners) listener();
}
function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

let theme: ThemeChoice = parseTheme(read(KEYS.theme));
let plainLabels = read(KEYS.plain) === "true";

function parseTheme(value: string | null): ThemeChoice {
  return value === "dark" || value === "light" ? value : "system";
}

/** Applies the theme choice to <html>; "system" follows the OS scheme. */
export function applyTheme(choice: ThemeChoice = theme): void {
  const root = document.documentElement;
  if (choice === "system") {
    delete root.dataset.theme;
  } else {
    root.dataset.theme = choice;
  }
}

export function setTheme(choice: ThemeChoice): void {
  theme = choice;
  write(KEYS.theme, choice === "system" ? null : choice);
  applyTheme(choice);
  emit();
}

/** The theme actually showing, resolving "system" against the OS. */
export function effectiveTheme(choice: ThemeChoice = theme): "dark" | "light" {
  if (choice !== "system") return choice;
  return window.matchMedia?.("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

export function setPlainLabels(value: boolean): void {
  plainLabels = value;
  write(KEYS.plain, value ? "true" : null);
  emit();
}

export function useTheme(): ThemeChoice {
  return useSyncExternalStore(subscribe, () => theme);
}

export function usePlainLabels(): boolean {
  return useSyncExternalStore(subscribe, () => plainLabels);
}

export function lastProject(): string | null {
  return read(KEYS.project);
}

export function rememberProject(projectId: string): void {
  write(KEYS.project, projectId);
}
