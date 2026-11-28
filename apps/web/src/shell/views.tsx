import type { ReactNode } from "react";
import {
  BlueprintIcon,
  BoltIcon,
  FlaskIcon,
  MapIcon,
  PulseIcon,
  SlabIcon,
  StitchIcon,
} from "../components/icons";
import { LABELS, type Label } from "../labels";

// Every Project view, in rail order, with its "g" shortcut. The rail, the
// shortcut sheet, and the command palette all read this list.

export type ProjectPath =
  | "/p/$project"
  | "/p/$project/board"
  | "/p/$project/stitches"
  | "/p/$project/map"
  | "/p/$project/blueprints"
  | "/p/$project/pulse"
  | "/p/$project/feed";

export interface ProjectView {
  key: string;
  to: ProjectPath;
  label: Label;
  text: string;
  icon: ReactNode;
  exact?: boolean;
}

export const PROJECT_VIEWS: ProjectView[] = [
  { key: "o", to: "/p/$project", label: LABELS.lab, text: "Lab", icon: <FlaskIcon />, exact: true },
  { key: "b", to: "/p/$project/board", label: LABELS.board, text: "Board", icon: <SlabIcon /> },
  {
    key: "s",
    to: "/p/$project/stitches",
    label: LABELS.stitchBoard,
    text: "Stitches",
    icon: <StitchIcon />,
  },
  { key: "m", to: "/p/$project/map", label: LABELS.stitchMap, text: "Map", icon: <MapIcon /> },
  {
    key: "w",
    to: "/p/$project/blueprints",
    label: LABELS.blueprints,
    text: "Plans",
    icon: <BlueprintIcon />,
  },
  { key: "p", to: "/p/$project/pulse", label: LABELS.pulse, text: "Pulse", icon: <PulseIcon /> },
  { key: "f", to: "/p/$project/feed", label: LABELS.feed, text: "Feed", icon: <BoltIcon /> },
];
