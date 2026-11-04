export type FlowStatus = "on" | "off" | "draft";
export type JobStatus = "pending" | "sending" | "sent" | "skipped" | "failed" | "canceled";

export interface Trigger {
  on: "order_paid" | "after_step" | "tracking";
  step?: string;
  event?: string;
}

export interface Delay {
  hours: number;
  business?: boolean;
}

export interface Step {
  id: string;
  title: string;
  subject: string;
  template: string;
  note: string;
  status: FlowStatus;
  trigger: Trigger;
  delay: Delay;
  conditions?: string[];
  window_days?: number;
  immediate?: boolean;
}

export interface StepView extends Step {
  effective: FlowStatus;
  timing: string;
  counts: Record<string, number> | null;
  held: number;
}

export interface StoreConfig {
  id: string;
  name: string;
  from: string;
  reply_to: string;
  site: string;
  timezone: string;
  send_window: { start: number; end: number };
  holidays?: string[];
  shopify_domain?: string;
}

export interface FlowView {
  store: StoreConfig;
  steps: StepView[];
  sender: string;
}

export interface Tenant {
  id: string;
  name: string;
  role: string;
}

export interface Me {
  email: string;
  tenants: Tenant[];
  stores: string[];
}

export interface OrderItem {
  title: string;
  variant?: string;
  image?: string;
  qty: number;
}

export interface Job {
  id: number;
  order_id: number;
  store_id: string;
  step_id: string;
  due_at: string;
  status: JobStatus;
  held: boolean;
  attempts: number;
  reason?: string;
  message_id?: string;
  sent_at?: string;
}

export interface TrackingEvent {
  id: number;
  order_id: number;
  event: string;
  label: string;
  source: string;
  at: string;
}

export interface Order {
  id: number;
  store_id: string;
  external_id: string;
  name: string;
  email: string;
  customer_name: string;
  currency: string;
  total: string;
  country: string;
  items: OrderItem[] | null;
  paid_at: string;
  tracking_number?: string;
  carrier?: string;
  tracking_url?: string;
  dispatched_at?: string;
  canceled: boolean;
  created_at: string;
}

export interface OrderView extends Order {
  jobs: Job[] | null;
  events: TrackingEvent[] | null;
}

export interface Passkey {
  id: string;
  name: string;
  created_at: string;
  last_used_at?: string;
}
