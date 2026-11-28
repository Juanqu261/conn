import { Dialog } from "radix-ui";
import { CrossIcon } from "../components/icons";
import { GO_KEYS } from "./shortcuts";

export function HelpSheet({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange(open: boolean): void;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog">
          <header className="dialog-header">
            <Dialog.Title className="dialog-title">Keyboard shortcuts</Dialog.Title>
            <Dialog.Close className="icon-button" aria-label="Close">
              <CrossIcon />
            </Dialog.Close>
          </header>
          <Dialog.Description className="dialog-description">
            Single-key shortcuts pause while you type in a field.
          </Dialog.Description>
          <dl className="shortcut-list">
            <div>
              <dt>
                <kbd className="kbd">Ctrl K</kbd> or <kbd className="kbd">⌘ K</kbd>
              </dt>
              <dd>Search the Lab</dd>
            </div>
            {Object.entries(GO_KEYS).map(([key, text]) => (
              <div key={key}>
                <dt>
                  <kbd className="kbd">g</kbd> then <kbd className="kbd">{key}</kbd>
                </dt>
                <dd>{text}</dd>
              </div>
            ))}
            <div>
              <dt>
                <kbd className="kbd">?</kbd>
              </dt>
              <dd>This sheet</dd>
            </div>
          </dl>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
