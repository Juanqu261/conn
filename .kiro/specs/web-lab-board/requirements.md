# Requirements

Milestone W2 of Conn Lab. Acceptance goal: a member can see on one board where every active Intent stands and why. A human can open a collision, read both sides, and record a binding decision in the browser with the same authority and provenance as `conn app`. A reviewer can read an Intent's proof on one page shaped like a pull request.

This milestone depends on `web-lab-foundation` (shell, session, CSRF, stream, design system) and on decision 0013. It adds the Lab's first writes: collision decisions and Charter decisions by the authenticated Human. That makes it the first production slice of `authenticated-browser-binding-review`.

## 1. Derived Intent board ("The Slab")

**User story:** As a member, I want a kanban board of active Intents whose columns reflect what Conn actually knows, so that the board can never be out of date or gamed.

### Acceptance criteria

1. THE SYSTEM SHALL compute each Intent's lane on the backend from its current revision with one deterministic rule set, in this order of precedence:
   - **Buried** ("Released"): the Intent was released within the board's window (default 14 days)
   - **Stitching** ("Needs a decision"): the current revision has a collision in state `open` or `under_review`
   - **Assembling** ("In progress"): the current revision has no head revision or no observed analysis
   - **Charging** ("Gathering proof"): at least one acceptance criterion of the Intent's Objective revision lacks passing, non-stale evidence for this Intent
   - **Alive** ("Ready for review"): none of the above
2. THE SYSTEM SHALL return with every card the rule that placed it and the facts behind it (for example "2 of 5 acceptance criteria passing; missing `fr-003`, `fr-004`, `fr-005`")
3. WHEN an Objective revision has no acceptance criteria
   THE SYSTEM SHALL skip the Charging rule for its Intents and SHALL say so on the card
4. THE SYSTEM SHALL exclude superseded Intents and SHALL never place an Intent in more than one lane
5. THE SYSTEM SHALL serve the board as a versioned `board-view` read model with the event cursor it reflects, so that `conn app` and the CLI can adopt the same lanes later without a second implementation

## 2. Board cards and filters

**User story:** As a member, I want each card to answer "what, whose, where, how far" at a glance.

### Acceptance criteria

1. EACH card SHALL show the Intent name, the owner's `@login` and avatar, the Objective title, the repository and branch, the number of open collisions with neck-bolt markers, evidence progress as passing over required acceptance criteria, and staleness of the latest evidence
2. THE SYSTEM SHALL let the member group the board into swimlanes by Objective or by repository, and filter by "Mine", Objective, repository, and lane
3. THE SYSTEM SHALL keep filters in the URL so that a filtered board can be shared
4. THE SYSTEM SHALL NOT show per-person totals, per-person lane counts, ages ranked by person, or any leaderboard; lane totals are Project-level only
5. THE SYSTEM SHALL render the board as a labelled list per lane for screen readers and below 768 px, preserving lane order

## 3. No manual movement of Intents

**User story:** As a Project owner, I want the board to report reality, not opinions, so that "Ready for review" always means the proof is there.

### Acceptance criteria

1. THE SYSTEM SHALL NOT let a member drag an Intent card between lanes or set its lane
2. WHEN a member tries to drag an Intent card
   THE SYSTEM SHALL keep the card in place and SHALL show what would move it (for example "Attach passing evidence for `fr-004` to move this to Alive")
3. WHEN a streamed event changes an Intent's lane
   THE SYSTEM SHALL move the card with a short transition, or instantly under reduced motion

## 4. "It's alive!"

**User story:** As a member watching the board, I want a moment of delight when work becomes ready for review.

### Acceptance criteria

1. WHEN a streamed event moves an Intent into Alive while the board is open
   THE SYSTEM SHALL play one lightning flash on that card of at most 400 ms and announce "Ready for review: <Intent name>" through a polite live region
2. THE SYSTEM SHALL NOT play the effect on page load, for Intents that were already Alive, or more than once per Intent revision
3. UNDER reduced motion THE SYSTEM SHALL show only the announcement and a static voltage outline

## 5. Collision board ("Stitches")

**User story:** As a human who decides collisions, I want a board of collisions by state, where moving a card records a decision only after I have read both sides and confirmed.

### Acceptance criteria

1. THE SYSTEM SHALL show collisions in the columns Open, Under review, Resolved, Dismissed, and Stale, each card naming the rule, both Intents and owners, matched subjects, severity, confidence, and the latest combination-check outcome
2. WHEN a member drops an Open or Under review card onto Resolved, Dismissed, or Under review, or uses the equivalent "Decide…" keyboard action
   THE SYSTEM SHALL open a decision review dialog and SHALL change nothing until the member confirms
3. THE decision review dialog SHALL show the Project, both Intents at their exact revisions, the matched subjects, the rule explanation, every proposal labelled with its author and whether it was agent-authored, the latest combination check, the decision kinds valid for the target column (Resolved: `dependency`, `sequencing`, `contract_agreement`, `accepted_risk`; Dismissed: `false_positive`; Under review: `revise_objective`, `revise_blueprint`, `other`), the consequence of each kind on collision state, and a required rationale
4. WHEN the member confirms
   THE SYSTEM SHALL submit the decision directly from the browser to the backend with the collision revisions the dialog showed, and THE backend SHALL reject it without effect if either Intent revision or the collision state changed since
5. WHEN the backend rejects a stale decision
   THE SYSTEM SHALL show what changed and reopen the dialog on current state; it SHALL NOT resubmit automatically
6. THE SYSTEM SHALL record the deciding Human, the `browser` session kind as the decision surface, the exact revisions, and the timestamp, as decision 0001 requires
7. THE SYSTEM SHALL NOT allow dragging a card out of Resolved, Dismissed, or Stale; those states change only through a new decision or a new Intent revision, as today
8. THE SYSTEM SHALL keep `conn app` and `conn resolution decide` working unchanged

## 6. Specimen page (Intent)

**User story:** As a reviewer, I want one page per Intent that reads like a pull request, so that I can trust or question an agent's change quickly.

### Acceptance criteria

1. THE SYSTEM SHALL show a header with the Intent name, owner, lane and the reason for it, Objective revision, repository, branch, base and head revisions, and a "View on GitHub" link built from the repository remote and branch
2. THE SYSTEM SHALL provide the tabs Overview, Claims, Stitches, Evidence, Review packet, and (when `web-lab-seance` ships) Séance
3. Overview SHALL render the summary Markdown, assumptions, and an acceptance-criteria checklist with each criterion's evidence state, provenance (`observed` on a participant's machine, `reported`, or CI), and staleness
4. Claims SHALL show declared and observed claims side by side, with scope drift and changed files lacking a declared claim highlighted, using the review packet's definitions
5. Evidence SHALL show a timeline of attached evidence, newest first, with kind, state, acceptance key, provenance, commit, and output hash, and SHALL never show check logs
6. Review packet SHALL render the generated review packet Markdown, offer "Copy Markdown", and state that it is generated on demand and not stored
7. THE SYSTEM SHALL show a revision selector and SHALL label every view with the revision it shows

## 7. Stitch page (collision)

**User story:** As a human deciding a collision, I want both sides of the stitch next to each other.

### Acceptance criteria

1. THE SYSTEM SHALL show both Intents side by side with the matched subjects drawn as stitches between their claims
2. THE SYSTEM SHALL show the rule explanation, proposals in order, the decision history, combination checks with their outcomes, and the `conn verify-pair` command to run locally
3. THE SYSTEM SHALL offer the same "Decide…" action and review dialog as the collision board
4. THE SYSTEM SHALL present every proposal as non-binding and SHALL never pre-select a proposal's suggested kind as the decision

## 8. Stitch Map

**User story:** As a member, I want to see the shape of concurrent work: which Intents are joined by collisions and around which subjects.

### Acceptance criteria

1. THE SYSTEM SHALL draw active Intents as nodes and collisions as edges between them, with edge style by state (dashed stitch for open, dotted for under review, solid for resolved, faded for dismissed) and colour by state
2. THE SYSTEM SHALL offer a subject layer that adds the matched subjects as small nodes joined to the Intents that claim them
3. THE SYSTEM SHALL cluster nodes by Objective and SHALL let the member focus one Intent and its neighbours
4. Selecting a node or edge SHALL open its Specimen or Stitch page
5. THE SYSTEM SHALL provide an equivalent table view and SHALL make the graph operable by keyboard
6. THE SYSTEM SHALL stay within the radar view's bounds and SHALL say when the graph omits items

## 9. Blueprints and Charter decisions

**User story:** As a Human assigned a Cell Charter, I want to accept or reject my own Charter in the browser after reading exactly what I commit to.

### Acceptance criteria

1. THE SYSTEM SHALL show each Objective revision with its key points and evidence coverage per acceptance criterion, and each Blueprint revision with its Cells, assignees, and acceptance status
2. WHEN the signed-in Human is the assignee of a pending Charter
   THE SYSTEM SHALL offer Accept and Reject, opening a review dialog that shows the immutable Charter snapshot, the Blueprint revision, and the activation consequence
3. THE SYSTEM SHALL submit the outcome directly to the existing Charter endpoints with the exact revision, and SHALL reject stale submissions without effect
4. THE SYSTEM SHALL NOT offer Accept or Reject on another Human's Charter, and THE backend SHALL refuse it regardless of the client

## 10. Pulse

**User story:** As a Project owner, I want Project insights as charts so that I can see whether coordination is improving.

### Acceptance criteria

1. THE SYSTEM SHALL chart every Project-insights measure for 7, 30, or 90 days, with the window shown
2. WHERE a measure has fewer samples than the insights minimum
   THE SYSTEM SHALL show "Not enough data yet" instead of a chart
3. THE SYSTEM SHALL contain no person, per-person breakdown, or filter by person, matching the insights contract
4. EACH chart SHALL have a table alternative
