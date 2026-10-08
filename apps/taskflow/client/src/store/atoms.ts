import { atom, createStore } from "jotai/vanilla";
import {
  api,
  type ConditionTask,
  type ExecutionPage,
  type Overview,
  type RiskRecord,
  type ScheduledTaskHint,
} from "@/api/client";

export const store = createStore();

export const overviewAtom = atom<Overview | null>(null);
export const scheduledTasksAtom = atom<ScheduledTaskHint[]>([]);
export const conditionTasksAtom = atom<ConditionTask[]>([]);
export const executionsAtom = atom<ExecutionPage>({
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
});
export const recordsAtom = atom<{ items: RiskRecord[]; total: number }>({
  items: [],
  total: 0,
});

export interface ExecutionFilter {
  page: number;
  pageSize: number;
  taskType: string;
  status: string;
}

export const execFilterAtom = atom<ExecutionFilter>({
  page: 1,
  pageSize: 15,
  taskType: "",
  status: "",
});

export interface Toast {
  seq: number;
  kind: "success" | "error" | "info";
  text: string;
}

export const toastAtom = atom<Toast | null>(null);
let toastSeq = 0;

export function showToast(kind: Toast["kind"], text: string) {
  toastSeq += 1;
  store.set(toastAtom, { seq: toastSeq, kind, text });
}

export function apiErrorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

export async function refreshOverview(): Promise<void> {
  store.set(overviewAtom, await api.overview());
}

export async function refreshScheduled(): Promise<void> {
  store.set(scheduledTasksAtom, await api.listScheduled());
}

export async function refreshCondition(): Promise<void> {
  store.set(conditionTasksAtom, await api.listCondition());
}

export async function refreshExecutions(
  override?: Partial<ExecutionFilter>,
): Promise<void> {
  const filter = { ...store.get(execFilterAtom), ...override };
  store.set(execFilterAtom, filter);
  const page = await api.listExecutions({
    page: filter.page,
    page_size: filter.pageSize,
    task_type: filter.taskType,
    status: filter.status,
  });
  store.set(executionsAtom, page);
}

export async function refreshRecords(): Promise<void> {
  store.set(recordsAtom, await api.listRecords(1, 50));
}

export const busyAtom = atom(false);
