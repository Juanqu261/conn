// The subset of the canonical /v1 contracts (contracts/v1) that Conn Lab
// reads. The JSON Schemas remain the source of truth; these types only
// describe what the Lab renders.

export type UUID = string;
export type Timestamp = string;

export interface Envelope<T> {
  data: T;
  as_of_event?: number;
}

export interface ApiErrorBody {
  error: { code: string; message: string; pointer?: string };
}

export interface Human {
  human_id: UUID;
  github_user_id: number;
  github_login: string;
}

export interface WebSession {
  human: Human;
  csrf_token: string;
  expires_at: Timestamp;
}

export type LabSetting = "CONN_GITHUB_CLIENT_ID" | "CONN_GITHUB_CLIENT_SECRET" | "CONN_PUBLIC_URL";

export interface WebLabStatus {
  lab_built: boolean;
  sign_in_available: boolean;
  missing_settings: LabSetting[];
  lab_url?: string;
}

export interface Project {
  project_id: UUID;
  name: string;
  owner_human_id: UUID;
  created_at: Timestamp;
}

export type Role = "owner" | "member";

export interface ProjectList {
  projects: Array<{ project: Project; role: Role }>;
}

export interface ProjectMemberList {
  project_id: UUID;
  members: Array<{
    human: Human;
    membership: { project_id: UUID; human_id: UUID; role: Role; added_at: Timestamp };
  }>;
}

export interface IntentRevision {
  intent_id: UUID;
  project_id: UUID;
  participant_id: UUID;
  revision: number;
  objective_id: UUID;
  objective_revision: number;
  repository_id?: UUID;
  branch_ref?: string;
  base_revision_ref?: string;
  head_revision_ref?: string;
  summary_markdown: string;
  assumptions?: string[];
  declared_claims?: SemanticClaimCandidate[];
  blueprint_id?: UUID;
  blueprint_revision?: number;
  charter_id?: UUID;
  created_at: Timestamp;
}

export interface SemanticClaimCandidate {
  subject_kind: string;
  normalized_subject: string;
  display_name: string;
  relation: string;
  provenance_kind: string;
  confidence?: number;
}

export interface SemanticClaim extends SemanticClaimCandidate {
  claim_id: UUID;
  project_id: UUID;
  intent_id: UUID;
  intent_revision: number;
}

export type IntentStatus = "active" | "released" | "superseded";

export interface IntentView {
  intent_revision: IntentRevision;
  status: IntentStatus;
  owner_human_id?: UUID;
  advisory_observations?: string[];
}

export interface IntentList {
  project_id: UUID;
  intents: IntentView[];
}

export type CollisionRule =
  | "shared_declared_invariant"
  | "shared_exported_symbol"
  | "contract_producer_consumer"
  | "schema_resource_overlap";

export type CollisionState = "open" | "under_review" | "resolved" | "dismissed" | "stale";
export type CombinationOutcome = "passing" | "failing" | "merge_conflict" | "incomplete";

export interface CollisionAssessment {
  collision_id: UUID;
  left_intent_id: UUID;
  left_intent_revision: number;
  right_intent_id: UUID;
  right_intent_revision: number;
  rule_id: CollisionRule;
  severity: "info" | "amber" | "red";
  confidence: number;
  state: CollisionState;
  matched_subjects: string[];
  latest_combination_outcome?: CombinationOutcome;
  decision_ref?: UUID;
  explanation_markdown: string;
  created_at: Timestamp;
  superseded_at?: Timestamp;
}

export interface RepositoryConnection {
  repository_id: UUID;
  host_kind: "github" | "generic_git";
  canonical_remote: string;
  default_branch?: string;
}

export type EvidenceState =
  | "passing"
  | "failing"
  | "missing"
  | "stale"
  | "inaccessible"
  | "unknown";

export interface EvidenceCoverage {
  objective_id: UUID;
  objective_revision: number;
  acceptance_key: string;
  state: EvidenceState;
  evidence_refs: UUID[];
  provenance?: "observed_run" | "reported";
  stale?: boolean;
}

export interface RadarObjective {
  objective_id: UUID;
  revision: number;
  title: string;
  source_kind?: "kiro_spec" | "speckit_feature" | "github_issue";
}

export interface RadarView {
  project: Project;
  as_of_event: number;
  objectives: RadarObjective[];
  repositories: RepositoryConnection[];
  active_intents: IntentRevision[];
  semantic_claims?: SemanticClaim[];
  open_collisions: CollisionAssessment[];
  decided_collisions?: CollisionAssessment[];
  active_blueprint?: { blueprint_id: UUID; revision: number };
  open_questions: string[];
  assumptions: string[];
  evidence_coverage: EvidenceCoverage[];
  known_unknowns: string[];
}

export interface EventEnvelope {
  project_id: UUID;
  event_sequence: number;
  event_id: UUID;
  event_type: string;
  entity_type: string;
  entity_id: UUID;
  actor_human_id: UUID;
  occurred_at: Timestamp;
  payload: Record<string, unknown>;
}

// ---------- web-lab-board read models ----------

export type Lane = "assembling" | "stitching" | "charging" | "alive" | "buried";

export type BoardRule =
  | "released_recently"
  | "open_collision"
  | "no_head_revision"
  | "no_observed_analysis"
  | "acceptance_incomplete"
  | "no_acceptance_criteria"
  | "ready";

export interface BoardCard {
  intent_id: UUID;
  intent_revision: number;
  status: "active" | "released";
  summary_line: string;
  owner_human_id?: UUID;
  objective_id: UUID;
  objective_revision: number;
  repository_id?: UUID;
  branch_ref?: string;
  base_revision_ref?: string;
  head_revision_ref?: string;
  open_collision_ids: UUID[];
  acceptance: { required: number; passing: number; missing_keys: string[] };
  latest_evidence_at?: Timestamp;
  evidence_stale: boolean;
  released_at?: Timestamp;
  reason: { rule: BoardRule; facts_markdown: string; next_step?: string };
}

export interface BoardLane {
  lane: Lane;
  count: number;
  cards: BoardCard[];
}

export interface BoardView {
  board_version: 1;
  project_id: UUID;
  as_of_event: number;
  window_days: number;
  lanes: BoardLane[];
  omitted: number;
}

export interface CollisionList {
  project_id: UUID;
  state: CollisionState;
  collisions: CollisionAssessment[];
  truncated: boolean;
}

export type ResolutionKind =
  | "dependency"
  | "sequencing"
  | "contract_agreement"
  | "accepted_risk"
  | "false_positive"
  | "revise_objective"
  | "revise_blueprint"
  | "other";

export interface ResolutionProposal {
  proposal_id: UUID;
  collision_id: UUID;
  proposed_by_participant_id?: UUID;
  proposed_by_human_id?: UUID;
  kind: ResolutionKind;
  rationale_markdown: string;
  created_at: Timestamp;
}

export type AdapterKind = "kiro" | "mcp" | "cli" | "ci" | "github" | "other_registered_adapter";

export interface ResolutionProposalList {
  project_id: UUID;
  collision_id: UUID;
  proposals: ResolutionProposal[];
  authors?: Array<{ participant_id: UUID; human_id: UUID; adapter_kind: AdapterKind }>;
}

export interface ResolutionDecisionInput {
  selected_proposal_id?: UUID;
  kind: ResolutionKind;
  binding: boolean;
  rationale_markdown: string;
  expected_left_intent_revision: number;
  expected_right_intent_revision: number;
  expected_state: CollisionState;
}

export interface ResolutionDecision {
  decision_id: UUID;
  collision_id: UUID;
  selected_proposal_id?: UUID;
  decided_by_human_id: UUID;
  kind: ResolutionKind;
  binding: boolean;
  rationale_markdown: string;
  created_at: Timestamp;
}

export interface CombinationSide {
  intent_id: UUID;
  intent_revision: number;
  branch_ref: string;
  commit: string;
}

export interface CombinationCheck {
  check_id: UUID;
  collision_id: UUID;
  left: CombinationSide;
  right: CombinationSide;
  diagnostics: { ran: boolean; new_count: number; truncated: boolean };
  outcome: CombinationOutcome;
  created_at: Timestamp;
}

export interface ConflictContextPack {
  collision: CollisionAssessment;
  objective: { objective_id: UUID; revision: number; title: string };
  intents: IntentRevision[];
  claims: SemanticClaim[];
  uncertainties: string[];
  existing_decision_refs?: UUID[];
  combination_checks?: CombinationCheck[];
}

export interface EvidenceObservation {
  method: string;
  runner: string;
  check_key: string;
  commit: string;
  worktree_dirty: boolean;
  exit_code: number;
  timed_out: boolean;
  duration_ms: number;
  output_sha256: string;
  started_at: Timestamp;
}

export interface EvidenceReference {
  evidence_id: UUID;
  intent_id?: UUID;
  objective_id: UUID;
  objective_revision: number;
  acceptance_key?: string;
  kind: string;
  state: EvidenceState;
  locator: string;
  content_hash?: string;
  summary?: string;
  observation?: EvidenceObservation;
  observed_at: Timestamp;
}

export interface EvidenceReferenceList {
  project_id: UUID;
  intent_id: UUID;
  evidence: EvidenceReference[];
}

export interface DriftSubject {
  subject_kind: string;
  normalized_subject: string;
  display_name: string;
  relation: string;
}

export type Provenance = "observed_run" | "reported";

export interface ReviewEvidence {
  id: UUID;
  kind: string;
  state: EvidenceState;
  provenance: Provenance;
  commit?: string;
  stale: boolean;
  dirty: boolean;
  observed_at: Timestamp;
}

export interface ReviewPacket {
  packet_version: number;
  generated_at_event: number;
  intent: {
    id: UUID;
    revision: number;
    owner_login: string;
    active: boolean;
    summary: string;
    assumptions: string[];
  };
  objective: { id: UUID; revision: number; title: string };
  git: { available: boolean; branch?: string; base?: string; head?: string };
  acceptance: Array<{
    key: string;
    statement: string;
    state: EvidenceState;
    provenance?: Provenance;
    evidence: ReviewEvidence[];
  }>;
  drift: {
    analysis: { status: "complete" | "incomplete" | "not_analyzed"; intent_revision?: number };
    matched: DriftSubject[];
    declared_not_observed: DriftSubject[];
    observed_not_declared: DriftSubject[];
    changed_files_without_claim: Array<{ path: string; written_by_agent: boolean }>;
    not_observable: Array<
      DriftSubject & { acceptance_key?: string; evidence_state?: EvidenceState }
    >;
  };
  collisions: Array<{ id: UUID; rule_id: CollisionRule; state: CollisionState }>;
  integrity_notes: string[];
  open_questions: string[];
  known_unknowns: string[];
}

export type KeyPointKind =
  | "outcome"
  | "acceptance_criterion"
  | "invariant"
  | "constraint"
  | "question";

export interface Objective {
  objective_id: UUID;
  revision: number;
  title: string;
  overview_markdown: string;
  accountable_human_id: UUID;
  key_points?: Array<{ key: string; kind: KeyPointKind; statement: string }>;
  created_at: Timestamp;
}

export type BoundaryKind = "responsibility" | "exclusion" | "deliverable" | "evidence_obligation";

export interface CellCharter {
  cell_id: UUID;
  accountable_human_id: UUID;
  title: string;
  overview_markdown: string;
  objective_point_refs?: string[];
  boundary_points?: Array<{ key: string; kind: BoundaryKind; statement: string }>;
  dependencies?: Array<{ cell_id: UUID; summary: string }>;
}

export type BlueprintStatus = "proposed" | "active" | "rejected" | "superseded";

export interface BlueprintRevisionView {
  blueprint_revision: {
    blueprint_id: UUID;
    revision: number;
    objective_id: UUID;
    objective_revision: number;
    proposed_by_human_id: UUID;
    created_at: Timestamp;
    cell_charters: CellCharter[];
  };
  status: BlueprintStatus;
}

export type CellDecision = "pending" | "accept" | "reject";

export interface BlueprintRevisionSummary {
  revision: number;
  status: "proposed" | "active" | "rejected";
  objective_id: UUID;
  objective_revision: number;
  cells: Array<{
    cell_id: UUID;
    accountable_human_id: UUID;
    title: string;
    decision: CellDecision;
  }>;
}

export interface ProjectStatus {
  project: Project;
  role: Role;
  objectives: Array<{ objective_id: UUID; current_revision: number }>;
  blueprints: Array<{
    blueprint_id: UUID;
    current_revision: number;
    revisions: BlueprintRevisionSummary[];
  }>;
}

export interface Ratio {
  numerator: number;
  denominator: number;
  status: "value" | "insufficient_data";
  value?: number;
}

export interface Median {
  sample_size: number;
  status: "value" | "insufficient_data";
  value?: number;
}

export interface ProjectInsights {
  project_id: UUID;
  since_days: number;
  window_start: Timestamp;
  window_end: Timestamp;
  minimum_denominator: number;
  collisions_opened: Array<{ rule_id: CollisionRule; count: number }>;
  decisions: Array<{ rule_id: CollisionRule; kind: ResolutionKind; count: number }>;
  false_positive_share: Array<{ rule_id: CollisionRule; share: Ratio }>;
  median_hours_to_first_decision: Median;
  decided_before_release: Ratio;
  undeclared_observed_changes: Ratio;
  acceptance_by_provenance: { observed_run: number; reported: number; missing: number };
  combination_checks: Array<{ outcome: CombinationOutcome; count: number }>;
}
