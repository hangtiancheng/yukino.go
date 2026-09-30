import { ensureToaster, type ToastType } from "../components/toaster";

export function showToast(message: string, type: ToastType = "info") {
  ensureToaster().push(message, type);
}
