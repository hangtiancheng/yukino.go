import { customElement, state } from "@yukino.js/lit-jsx";
import { AtomElement } from "@/components/base";
import { Icon } from "@/components/icon";
import { PageHeader, formatTime } from "@/components/ui";
import { api, type ScheduledTaskHint } from "@/api/client";
import {
  apiErrorMessage,
  refreshOverview,
  refreshScheduled,
  scheduledTasksAtom,
  showToast,
  store,
} from "@/store/atoms";

interface FormState {
  id: number | null;
  name: string;
  description: string;
  cron_expr: string;
  timezone: string;
  prompt: string;
  model: string;
  enabled: boolean;
}

const emptyForm: FormState = {
  id: null,
  name: "",
  description: "",
  cron_expr: "0 10 * * *",
  timezone: "Asia/Shanghai",
  prompt: "",
  model: "",
  enabled: true,
};

@customElement("scheduled-page")
export class ScheduledPage extends AtomElement {
  @state() private form: FormState | null = null;
  @state() private busy = false;

  protected override setupWatches(): void {
    this.watch(scheduledTasksAtom);
  }

  protected override loadData(): void {
    refreshScheduled().catch((err) => showToast("error", apiErrorMessage(err)));
  }

  private openCreate() {
    this.form = { ...emptyForm };
  }

  private openEdit(task: ScheduledTaskHint) {
    this.form = {
      id: task.id,
      name: task.name,
      description: task.description,
      cron_expr: task.cron_expr,
      timezone: task.timezone || "Asia/Shanghai",
      prompt: task.prompt,
      model: task.model,
      enabled: task.enabled,
    };
  }

  private closeForm() {
    this.form = null;
  }

  private patch(partial: Partial<FormState>) {
    if (this.form) this.form = { ...this.form, ...partial };
  }

  private async submit() {
    const form = this.form;
    if (!form || this.busy) return;
    this.busy = true;
    try {
      if (form.id === null) {
        await api.createScheduled({
          name: form.name,
          description: form.description,
          cron_expr: form.cron_expr,
          timezone: form.timezone,
          prompt: form.prompt,
          model: form.model,
          enabled: form.enabled,
        });
        showToast("success", "Scheduled task created");
      } else {
        await api.updateScheduled(form.id, {
          name: form.name,
          description: form.description,
          cron_expr: form.cron_expr,
          timezone: form.timezone,
          prompt: form.prompt,
          model: form.model,
          enabled: form.enabled,
        });
        showToast("success", "Scheduled task updated");
      }
      this.closeForm();
      await refreshScheduled();
    } catch (err) {
      showToast("error", apiErrorMessage(err));
    } finally {
      this.busy = false;
    }
  }

  private async removeTask(task: ScheduledTaskHint) {
    if (!window.confirm(`Delete scheduled task "${task.name}"?`)) return;
    try {
      await api.deleteScheduled(task.id);
      showToast("success", "Deleted");
      await refreshScheduled();
    } catch (err) {
      showToast("error", apiErrorMessage(err));
    }
  }

  private async toggle(task: ScheduledTaskHint, enabled: boolean) {
    try {
      await api.updateScheduled(task.id, { enabled });
      await refreshScheduled();
    } catch (err) {
      showToast("error", apiErrorMessage(err));
    }
  }

  private async trigger(task: ScheduledTaskHint) {
    try {
      const resp = await api.triggerScheduled(task.id);
      showToast("success", `Execution #${resp.execution_id} queued`);
      refreshOverview().catch(() => undefined);
    } catch (err) {
      showToast("error", apiErrorMessage(err));
    }
  }

  private formModal() {
    const form = this.form;
    if (!form) return null;
    return (
      <div class="bg-base-content/20 fixed inset-0 z-40 flex items-center justify-center p-4 backdrop-blur-sm">
        <div
          class="card bg-base-100 max-h-[calc(100dvh-2rem)] w-full max-w-2xl overflow-y-auto shadow-xl"
          role="dialog"
          aria-modal="true"
          aria-label="Task editor"
        >
          <div class="card-body gap-4">
            <div class="flex items-center justify-between">
              <h3 class="text-lg font-semibold">
                {form.id === null ? "New task" : `Edit task #${form.id}`}
              </h3>
              <button
                aria-label="Close task editor"
                class="btn btn-circle btn-ghost btn-sm rounded-full"
                onClick={() => this.closeForm()}
              >
                <Icon name="x-circle" class="h-4 w-4" />
              </button>
            </div>

            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <label class="flex flex-col gap-1.5">
                <div class="text-base-content/70 text-xs">
                  <span class="text-xs">Task name</span>
                </div>
                <input
                  class="input input-sm w-full"
                  value={form.name}
                  onInput={(e: Event) =>
                    this.patch({ name: (e.target as HTMLInputElement).value })
                  }
                  placeholder="Daily MySQL Data Inspection"
                />
              </label>
              <label class="flex flex-col gap-1.5">
                <div class="text-base-content/70 text-xs">
                  <span class="text-xs">Timezone</span>
                </div>
                <input
                  class="input input-sm w-full"
                  value={form.timezone}
                  onInput={(e: Event) =>
                    this.patch({
                      timezone: (e.target as HTMLInputElement).value,
                    })
                  }
                  placeholder="Asia/Shanghai"
                />
              </label>
            </div>

            <label class="flex flex-col gap-1.5">
              <div class="text-base-content/70 text-xs">
                <span class="text-xs">Cron expression</span>
              </div>
              <input
                class="input input-sm w-full font-mono"
                value={form.cron_expr}
                onInput={(e: Event) =>
                  this.patch({
                    cron_expr: (e.target as HTMLInputElement).value,
                  })
                }
                placeholder="0 10 * * *"
              />
            </label>

            <label class="flex flex-col gap-1.5">
              <div class="text-base-content/70 text-xs">
                <span class="text-xs">Description</span>
              </div>
              <input
                class="input input-sm w-full"
                value={form.description}
                onInput={(e: Event) =>
                  this.patch({
                    description: (e.target as HTMLInputElement).value,
                  })
                }
              />
            </label>

            <label class="flex flex-col gap-1.5">
              <div class="text-base-content/70 text-xs">
                <span class="text-xs">Prompt</span>
              </div>
              <textarea
                class="textarea min-h-36 w-full text-sm"
                onInput={(e: Event) =>
                  this.patch({
                    prompt: (e.target as HTMLTextAreaElement).value,
                  })
                }
              >
                {form.prompt}
              </textarea>
            </label>

            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <label class="flex flex-col gap-1.5">
                <div class="text-base-content/70 text-xs">
                  <span class="text-xs">Model</span>
                </div>
                <input
                  class="input input-sm w-full"
                  value={form.model}
                  onInput={(e: Event) =>
                    this.patch({ model: (e.target as HTMLInputElement).value })
                  }
                  placeholder="Default model"
                />
              </label>
              <label class="flex cursor-pointer items-center gap-2 self-end pb-2">
                <input
                  type="checkbox"
                  class="toggle toggle-primary toggle-sm"
                  checked={form.enabled}
                  onChange={(e: Event) =>
                    this.patch({
                      enabled: (e.target as HTMLInputElement).checked,
                    })
                  }
                />
                <span class="text-sm">Enabled</span>
              </label>
            </div>

            <div class="card-actions mt-2 justify-end">
              <button
                class="btn btn-ghost btn-sm rounded-full"
                yukino-sentry-ev="scheduled-form-cancel"
                yukino-sentry-msg="Cancel scheduled task form"
                onClick={() => this.closeForm()}
              >
                Cancel
              </button>
              <button
                class={`btn btn-primary btn-sm rounded-full ${this.busy ? "loading" : ""}`}
                yukino-sentry-ev="scheduled-form-submit"
                yukino-sentry-msg={
                  form.id === null
                    ? "Create scheduled task"
                    : "Save scheduled task"
                }
                yukino-sentry-task-id={
                  form.id === null ? "new" : String(form.id)
                }
                onClick={() => this.submit()}
              >
                {form.id === null ? "Create" : "Save"}
              </button>
            </div>
          </div>
        </div>
      </div>
    );
  }

  render() {
    const tasks = store.get(scheduledTasksAtom);
    return (
      <div yukino-sentry-view="scheduled-tasks">
        <PageHeader
          title="Scheduled tasks"
          subtitle="Tasks that fire on a cron schedule and produce a report"
          actions={
            <button
              class="btn btn-primary btn-sm gap-1 rounded-full"
              yukino-sentry-ev="scheduled-create-open"
              yukino-sentry-msg="Open new scheduled task form"
              onClick={() => this.openCreate()}
            >
              <Icon name="plus" class="h-3.5 w-3.5" />
              New task
            </button>
          }
        />

        <div class="card bg-base-100 ring-base-300/60 ring-1">
          <div class="overflow-x-auto">
            <table class="table">
              <thead>
                <tr class="text-base-content/60 text-xs uppercase">
                  <th>ID</th>
                  <th>Task</th>
                  <th>Cron</th>
                  <th>Next fire</th>
                  <th>Last fire</th>
                  <th>Enabled</th>
                  <th class="text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {tasks.map((task) => (
                  <tr class="hover align-top">
                    <td class="font-mono text-xs">#{task.id}</td>
                    <td class="min-w-[200px]">
                      <div class="font-medium">{task.name}</div>
                      <div class="text-base-content/50 mt-0.5 max-w-[280px] truncate text-xs">
                        {task.description || ""}
                      </div>
                    </td>
                    <td>
                      <code class="bg-base-200 rounded px-1.5 py-0.5 text-xs">
                        {task.cron_expr}
                      </code>
                      <div class="text-base-content/50 mt-0.5 text-[11px]">
                        {task.timezone}
                      </div>
                    </td>
                    <td class="text-base-content/70 text-xs">
                      {task.next_fire_hint
                        ? formatTime(task.next_fire_hint)
                        : "-"}
                    </td>
                    <td class="text-base-content/70 text-xs">
                      {task.last_fire_at ? formatTime(task.last_fire_at) : "-"}
                    </td>
                    <td>
                      <input
                        aria-label={`Enable ${task.name}`}
                        type="checkbox"
                        class="toggle toggle-primary toggle-sm"
                        checked={task.enabled}
                        yukino-sentry-ev="scheduled-toggle"
                        yukino-sentry-msg={task.name}
                        yukino-sentry-task-id={String(task.id)}
                        yukino-sentry-enabled={String(!task.enabled)}
                        onChange={() => this.toggle(task, !task.enabled)}
                      />
                    </td>
                    <td>
                      <div class="flex justify-end gap-1">
                        <button
                          class="btn btn-primary btn-xs gap-1 rounded-full"
                          yukino-sentry-ev="scheduled-run"
                          yukino-sentry-msg={task.name}
                          yukino-sentry-task-id={String(task.id)}
                          onClick={() => this.trigger(task)}
                        >
                          <Icon name="play" class="h-3 w-3" />
                          Run Now
                        </button>
                        <button
                          class="btn btn-ghost btn-xs rounded-full"
                          yukino-sentry-ev="scheduled-edit-open"
                          yukino-sentry-msg={task.name}
                          yukino-sentry-task-id={String(task.id)}
                          onClick={() => this.openEdit(task)}
                        >
                          Edit
                        </button>
                        <button
                          aria-label={`Delete ${task.name}`}
                          class="btn btn-ghost btn-xs text-error rounded-full"
                          yukino-sentry-ev="scheduled-delete"
                          yukino-sentry-msg={task.name}
                          yukino-sentry-task-id={String(task.id)}
                          onClick={() => this.removeTask(task)}
                        >
                          <Icon name="trash-2" class="h-3 w-3" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
                {tasks.length === 0 ? (
                  <tr>
                    <td
                      colspan={7}
                      class="text-base-content/50 py-10 text-center text-sm"
                    >
                      No scheduled tasks yet - create one from the top-right
                      button
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </div>

        {this.formModal()}
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "scheduled-page": ScheduledPage;
  }
}
