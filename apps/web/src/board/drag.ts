import type { DraggableSyntheticListeners } from "@dnd-kit/core";
import type { KeyboardEventHandler, PointerEventHandler } from "react";

/**
 * dnd-kit's activators, split so a pointer can pick up the whole card while
 * the keyboard uses a dedicated handle button. The card's links stay plain
 * links.
 */
export function splitListeners(listeners: DraggableSyntheticListeners) {
  return {
    onPointerDown: listeners?.onPointerDown as PointerEventHandler<HTMLElement> | undefined,
    onKeyDown: listeners?.onKeyDown as KeyboardEventHandler<HTMLElement> | undefined,
  };
}
