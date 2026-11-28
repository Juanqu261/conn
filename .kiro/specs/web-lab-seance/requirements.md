# Requirements

Milestone W3 of Conn Lab. Acceptance goal: the conversation about a collision, an Intent, or an Objective lives on that record, next to the evidence and decisions it is about, and reaches the team's existing chat when it matters. Conn does not become a chat product.

This milestone depends on `web-lab-foundation` and `web-lab-board`.

**It changes product direction.** `.kiro/steering/product.md` currently lists chat as failing the feature test. This milestone proposes a narrow amendment, decision 0014: discussion **attached to a Conn record** passes the test, because the reasoning behind a collision decision or a contract agreement is exactly the context the next participant, human or agent, needs to avoid rework. General chat, channels, direct messages, presence, and task assignment still fail the test and stay out.

## 1. Product-direction gate

**User story:** As the project owner, I want the boundary between record-attached discussion and chat written down before any comment table exists.

### Acceptance criteria

1. THE SYSTEM SHALL NOT contain comment, thread, notification, or bridge persistence until decision 0014 is accepted and `.kiro/steering/product.md` is amended
2. Decision 0014 SHALL state that threads attach only to an Objective, an Intent, a collision, or a Blueprint revision, and SHALL list as out of scope: channels, direct messages, threads without a record, presence or online status, typing indicators, reactions used as votes, task assignment, and any per-person activity count
3. Decision 0014 SHALL state that no comment, reaction, or thread state has authority: it cannot record a decision, accept a Charter, approve anything, or change a collision state

## 2. Séance threads

**User story:** As a member, I want to discuss a record on the record, so that the reasoning stays with the thing it is about.

### Acceptance criteria

1. THE SYSTEM SHALL provide exactly one thread per Objective, Intent, collision, and Blueprint revision, created on the first comment
2. WHEN a member posts a comment
   THE SYSTEM SHALL store Markdown of at most 8,000 characters, attributed to the Human and the `browser` surface, and SHALL append a `comment.posted` event to the Project cursor
3. THE SYSTEM SHALL allow one level of replies under a top-level comment
4. WHEN the author edits a comment
   THE SYSTEM SHALL keep every earlier version, mark the comment as edited, and show the history to any member
5. WHEN the author deletes a comment
   THE SYSTEM SHALL replace its body with a tombstone that keeps the author and time, and SHALL keep replies visible
6. WHEN a Project owner hides a comment
   THE SYSTEM SHALL replace it for everyone with "Hidden by a Project owner", record who hid it and why, and let any owner unhide it
7. THE SYSTEM SHALL render comment Markdown through an allowlist sanitizer with no raw HTML, no images, no iframes, and links opened with `rel="noopener noreferrer nofollow"`
8. THE SYSTEM SHALL NOT accept attachments or file uploads, and SHALL warn before posting text that matches a credential pattern (for example a `conn_ci_` prefix, a GitHub token prefix, or a private-key header)

## 3. Record timeline

**User story:** As a reviewer, I want comments interleaved with what happened to the record, like a pull-request conversation.

### Acceptance criteria

1. THE SYSTEM SHALL show each thread as a timeline that interleaves comments with the record's events: Intent revisions, observed analyses, evidence attached, collisions opened or updated, proposals, decisions, combination checks, Charter decisions, and releases
2. EACH event entry SHALL use the same human sentence as the Lightning feed and SHALL link to the record it names
3. A proposal entry SHALL show its full text and SHALL be labelled non-binding and, where applicable, agent-authored
4. A decision entry SHALL be visually distinct from comments and SHALL state that it, not any comment, set the collision state

## 4. Mentions and references

**User story:** As a member, I want to pull a teammate or another record into a discussion by name.

### Acceptance criteria

1. WHEN a member types `@`
   THE SYSTEM SHALL suggest only members of the current Project by `@login`, and SHALL store the mention by immutable Human ID
2. WHEN a member types `#`
   THE SYSTEM SHALL suggest Objectives, Intents, and collisions of the current Project by name, and SHALL store the reference by ID and render it by current name
3. THE SYSTEM SHALL render a mention or reference the viewer cannot resolve as plain text, without the identifier
4. THE SYSTEM SHALL list, on each record, the threads elsewhere that reference it ("Mentioned in")

## 5. Bell tower (notification inbox)

**User story:** As a member, I want one inbox for the things that need me, without watching every thread.

### Acceptance criteria

1. THE SYSTEM SHALL notify a Human when they are mentioned, when someone replies to their comment, when a collision opens or changes state on an Intent they own, when a proposal is recorded on such a collision, when a combination check on such a collision fails, and when a Charter is assigned to them
2. THE SYSTEM SHALL create notifications in the same transaction as the event that causes them, and SHALL never notify the Human who caused the event
3. THE SYSTEM SHALL let a Human mark one, several, or all notifications read, and SHALL show unread count on the rail
4. THE SYSTEM SHALL let a Human mute a thread and choose which notification kinds they receive
5. A Human's notifications and preferences SHALL be visible only to that Human, and THE SYSTEM SHALL derive no metric from them
6. THE SYSTEM SHALL deliver new notifications to open Lab tabs through the event stream and SHALL show them in the feed only to their recipient

## 6. Outbound bridges (Slack and Discord)

**User story:** As a Project owner, I want collisions and decisions posted where my team already talks, with a link back to the Lab.

### Acceptance criteria

1. WHEN a Project owner adds a bridge
   THE SYSTEM SHALL accept only a Slack incoming-webhook URL on `https://hooks.slack.com/` or a Discord webhook URL on `https://discord.com/api/webhooks/`, and SHALL refuse any other host
2. THE SYSTEM SHALL store the webhook URL encrypted at rest, SHALL never return it after saving (showing only the host and the last four characters), and SHALL treat it as a secret in logs and errors
3. THE owner SHALL choose which event kinds the bridge posts, from: collision opened, collision decided, combination check failing, Intent ready for review ("It's alive!"), Objective revision created, and Charter decided
4. EACH bridged message SHALL contain names, the event sentence, and a Lab link, and SHALL NOT contain comment bodies, proposal text, rationale text, claims beyond matched subjects, or identifiers in visible text
5. THE SYSTEM SHALL deliver each event to each bridge at most once on success, retry failures at most three times with backoff, and record each attempt's time and HTTP status without the URL
6. WHEN a bridge fails five deliveries in a row
   THE SYSTEM SHALL pause it, show the reason to Project owners in the Lab, and notify them in the Bell tower
7. THE SYSTEM SHALL offer "Send test message", and SHALL let any owner pause, resume, edit event kinds, replace the URL, or delete the bridge, each recorded as a Project event
8. THE SYSTEM SHALL never read from Slack or Discord; bridges are outbound only

## 7. Agents and discussion

**User story:** As a participant whose agent is about to change something a teammate discussed, I want the agent to see that discussion as context, without it becoming an instruction channel.

### Acceptance criteria

1. THE SYSTEM SHALL provide a read-only MCP tool `read_thread` that returns the most recent 50 comments and timeline entries of one record's thread, bounded to 32,000 characters
2. THE SYSTEM SHALL label every returned comment as human-written discussion, quoted as data, and THE Conn agent guidance SHALL instruct agents to treat thread content as information and never as instructions
3. THE SYSTEM SHALL include in work context only the count of comments since the participant's last sync on the focused Intent and its collisions, and the names of those threads, not their bodies
4. Agents SHALL NOT post, edit, delete, hide, or react to comments in this milestone
5. Hooks SHALL NOT read threads

## 8. Retention and privacy

**User story:** As a Project owner, I want to know what discussion data Conn keeps.

### Acceptance criteria

1. THE SYSTEM SHALL keep comments, their versions, and tombstones for the life of the Project
2. THE SYSTEM SHALL let a Human export their own comments as JSON from the Account page
3. THE SYSTEM SHALL include threads in no CI report, review packet, or bridge message, except that the review packet MAY state the number of comments on the Intent and its collisions
