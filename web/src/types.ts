export type User = { id: string; name: string; email: string; role: string };
export type Project = {
  id: string;
  name: string;
  summary: string;
  repository: string;
  identity_key: string;
  updated_at: string;
};
export type Item = {
  id: string;
  project_id: string;
  external_key: string;
  kind: string;
  title: string;
  description: string;
  status: string;
  priority: string;
  parent_id: string;
  version: number;
  acceptance: string;
  evidence: string;
  next_step: string;
  actor: string;
  created_at: string;
  updated_at: string;
};
export type Event = {
  sequence: number;
  project_id: string;
  item_id: string;
  actor: string;
  action: string;
  summary: string;
  created_at: string;
};
export type Snapshot = {
  user: User;
  projects: Project[];
  items: Item[];
  events: Event[];
  cursor: number;
};
export type Token = { id: string; name: string; expires_at: string };
