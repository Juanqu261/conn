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
  created_at: Timestamp;
}

export type IntentStatus = "active" | "released" | "superseded";

export interface IntentView {
  intent_revision: IntentRevision;
  status: IntentStatus;
  owner_human_id?: UUID;
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
  explanation_markdown: string;
  created_at: Timestamp;
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
  open_collisions: CollisionAssessment[];
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
