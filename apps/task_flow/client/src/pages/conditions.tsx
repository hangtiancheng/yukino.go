import { customElement, state } from "@yukino.js/lit-jsx";
import { AtomElement } from "@/components/base";
import { Icon } from "@/components/icon";
import { PageHeader } from "@/components/ui";
import { api, type ConditionTask } from "@/api/client";
import {
  apiErrorMessage,
  conditionTasksAtom,
  refreshCondition,
  refreshOverview,
  showToast,
  store,
} from "@/store/atoms";

interface FormState {
  id: number | null;
  name: string;
  description: string;
  table_name: string;
  prompt: string;
  model: string;
  enabled: boolean;
}

const emptyForm: FormState = {
  id: null,
  name: "",
  description: "",
  table_name: "risk_records",
  prompt: "",
  model: "",
  enabled: true,
};

@customElement("conditions-page")
export class ConditionsPage extends AtomElement {
  @state() private form: FormState | null = null;
  @state() private busy = false;
  @state() private testRecordId = "";

  protected override setupWatches(): void {
    this.watch(conditionTasksAtom);
  }

  protected override loadData(): void {
    refreshCondition().catch((err) => showToast("error", apiErrorMessage(err)));
  }

  private patch(partial: Partial<FormState>) {
    if (this.form) this.form = { ...this.form, ...partial };
  }

  private async submit() {
    const form = this.form;
    if (!form || this.busy) return;
    this.busy = true;
    try {
      const payload = {
        name: form.name,
        description: form.description,
        table_name: form.table_name,
        prompt: form.prompt,
        model: form.model,
        enabled: form.enabled,
      };
      if (form.id === null) {
        await api.createCondition(payload);
        showToast("success", "Condition task created");
      } else {
        await api.updateCondition(form.id, payload);
        showToast("success", "Condition task updated");
      }
      this.form = null;
      await refreshCondition();
    } catch (err) {
      showToast("error", apiErrorMessage(err));
    } finally {
      this.busy = false;
    }
  }

  private async removeTask(task: ConditionTask) {
    if (!window.confirm(`Delete condition task "${task.name}"?`)) return;
    try {
      await api.deleteCondition(task.id);
      showToast("success", "Deleted");
      await refreshCondition();
    } catch (err) {
      showToast("error", apiErrorMessage(err));
    }
  }

  private async toggle(task: ConditionTask, enabled: boolean) {
    try {
      await api.updateCondition(task.id, { enabled });
      await refreshCondition();
    } catch (err) {
      showToast("error", apiErrorMessage(err));
    }
  }

  private async testRun(task: ConditionTask) {
    try {
      const recordId = this.testRecordId
        ? Number(this.testRecordId)
        : undefined;
      const resp = await api.testCondition(task.id, recordId);
      showToast(
        "success",
        `Started audit execution #${resp.execution_id} (record #${resp.record_id})`,
      );
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
                onClick={() => (this.form = null)}
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
                />
              </label>
              <label class="flex flex-col gap-1.5">
                <div class="text-base-content/70 text-xs">
                  <span class="text-xs">Watched table</span>
                </div>
                <input
                  class="input input-sm w-full font-mono"
                  value={form.table_name}
                  onInput={(e: Event) =>
                    this.patch({
                      table_name: (e.target as HTMLInputElement).value,
                    })
                  }
                />
              </label>
            </div>

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
                yukino-sentry-ev="conditions-form-cancel"
                yukino-sentry-msg="Cancel condition task form"
                onClick={() => (this.form = null)}
              >
                Cancel
              </button>
              <button
                class={`btn btn-primary btn-sm rounded-full ${this.busy ? "loading" : ""}`}
                yukino-sentry-ev="conditions-form-submit"
                yukino-sentry-msg={
                  form.id === null
                    ? "Create condition task"
                    : "Save condition task"
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
    const tasks = store.get(conditionTasksAtom);
    return (
      <div yukino-sentry-view="condition-tasks">
        <PageHeader
          title="Condition tasks"
          subtitle="Tasks triggered by inserts into a watched table"
          actions={
            <button
              class="btn btn-primary btn-sm gap-1 rounded-full"
              yukino-sentry-ev="conditions-create-open"
              yukino-sentry-msg="Open new condition task form"
              onClick={() => (this.form = { ...emptyForm })}
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
                  <th>Event</th>
                  <th>Enabled</th>
                  <th class="text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {tasks.map((task) => (
                  <tr class="hover align-top">
                    <td class="font-mono text-xs">#{task.id}</td>
                    <td class="min-w-[220px]">
                      <div class="font-medium">{task.name}</div>
                      <div class="text-base-content/50 mt-0.5 max-w-[320px] truncate text-xs">
                        {task.description || ""}
                      </div>
                    </td>
                    <td>
                      <span class="badge badge-secondary badge-outline badge-sm gap-1">
                        <Icon name="database" class="h-3 w-3" />
                        {task.event_type}
                      </span>
                      <div class="text-base-content/70 mt-1 font-mono text-xs">
                        {task.table_name}
                      </div>
                    </td>
                    <td>
                      <input
                        aria-label={`Enable ${task.name}`}
                        type="checkbox"
                        class="toggle toggle-primary toggle-sm"
                        checked={task.enabled}
                        yukino-sentry-ev="conditions-toggle"
                        yukino-sentry-msg={task.name}
                        yukino-sentry-task-id={String(task.id)}
                        yukino-sentry-enabled={String(!task.enabled)}
                        onChange={() => this.toggle(task, !task.enabled)}
                      />
                    </td>
                    <td>
                      <div class="flex flex-wrap items-center justify-end gap-1">
                        <div class="flex items-center gap-1">
                          <input
                            class="input input-bordered input-xs w-20"
                            placeholder="Record ID"
                            value={this.testRecordId}
                            onInput={(e: Event) =>
                              (this.testRecordId = (
                                e.target as HTMLInputElement
                              ).value)
                            }
                          />
                          <button
                            class="btn btn-accent btn-xs gap-1 rounded-full"
                            title="Run one audit against the given record (or the latest one)"
                            yukino-sentry-ev="conditions-test-run"
                            yukino-sentry-msg={task.name}
                            yukino-sentry-task-id={String(task.id)}
                            onClick={() => this.testRun(task)}
                          >
                            <Icon name="flask-conical" class="h-3 w-3" />
                            Test run
                          </button>
                        </div>
                        <button
                          class="btn btn-ghost btn-xs rounded-full"
                          yukino-sentry-ev="conditions-edit-open"
                          yukino-sentry-msg={task.name}
                          yukino-sentry-task-id={String(task.id)}
                          onClick={() =>
                            (this.form = {
                              id: task.id,
                              name: task.name,
                              description: task.description,
                              table_name: task.table_name,
                              prompt: task.prompt,
                              model: task.model,
                              enabled: task.enabled,
                            })
                          }
                        >
                          Edit
                        </button>
                        <button
                          aria-label={`Delete ${task.name}`}
                          class="btn btn-ghost btn-xs text-error rounded-full"
                          yukino-sentry-ev="conditions-delete"
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
                      colspan={5}
                      class="text-base-content/50 py-10 text-center text-sm"
                    >
                      No condition tasks yet
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
    "conditions-page": ConditionsPage;
  }
}
