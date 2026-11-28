// Themed names always travel with a plain caption, and the plain wording is
// the accessible name (web-lab-foundation requirement 4.5). The "Plain
// labels" preference shows only the plain wording.

export interface Label {
  themed: string;
  plain: string;
}

export const LABELS = {
  lab: { themed: "The Lab", plain: "Overview" },
  feed: { themed: "Lightning feed", plain: "Activity" },
  objectives: { themed: "Blueprints on the wall", plain: "Objectives" },
  specimens: { themed: "Specimens on the slab", plain: "Active Intents" },
  stitches: { themed: "Stitches", plain: "Collisions needing a decision" },
  pairChecks: { themed: "Failed grafts", plain: "Pair checks failing or incomplete" },
  questions: { themed: "Unanswered riddles", plain: "Open questions" },
  unknowns: { themed: "In the fog", plain: "Known unknowns" },
  account: { themed: "Account", plain: "Account" },
} satisfies Record<string, Label>;
