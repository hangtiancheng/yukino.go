import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  TakeoutApi,
  type EvaluationPayload,
  type FeedbackPayload,
  type TraceLabelPayload,
  type TraceListParams,
} from "@/lib/api";
import type { MealRequest } from "@/lib/schemas";

export function useSlotOptions() {
  return useQuery({
    queryKey: ["slot-options"],
    queryFn: TakeoutApi.slotOptions,
    staleTime: 5 * 60 * 1000,
  });
}

export function usePersonalMeals() {
  return useQuery({
    queryKey: ["meals", "personal"],
    queryFn: TakeoutApi.listPersonalMeals,
  });
}

export function usePublicMeals() {
  return useQuery({
    queryKey: ["meals", "public"],
    queryFn: TakeoutApi.listPublicMeals,
  });
}

export function useCreateMeal() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: MealRequest) =>
      TakeoutApi.createPersonalMeal(payload),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["meals", "personal"] }),
  });
}

export function useUpdateMeal() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      mealId,
      payload,
    }: {
      mealId: number;
      payload: MealRequest;
    }) => TakeoutApi.updatePersonalMeal(mealId, payload),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["meals", "personal"] }),
  });
}

export function useDeleteMeal() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (mealId: number) => TakeoutApi.deletePersonalMeal(mealId),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["meals", "personal"] }),
  });
}

export function useTraces(params: TraceListParams, enabled = true) {
  return useQuery({
    queryKey: ["traces", "list", params],
    queryFn: () => TakeoutApi.listTraces(params),
    enabled,
  });
}

export function useSessionTraces(sessionId: string, limit?: number) {
  return useQuery({
    queryKey: ["traces", "session", sessionId, limit],
    queryFn: () => TakeoutApi.listSessionTraces(sessionId, limit),
    enabled: sessionId.trim() !== "",
  });
}

export function useTraceDetail(traceId: string | null) {
  return useQuery({
    queryKey: ["traces", "detail", traceId],
    queryFn: () => TakeoutApi.getTrace(traceId as string),
    enabled: Boolean(traceId),
  });
}

export function useLabelTrace() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      traceId,
      payload,
    }: {
      traceId: string;
      payload: TraceLabelPayload;
    }) => TakeoutApi.labelTrace(traceId, payload),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["traces"] }),
  });
}

export function useEvaluate() {
  return useMutation({
    mutationFn: (payload: EvaluationPayload) => TakeoutApi.evaluate(payload),
  });
}

export function useSaveFeedback() {
  return useMutation({
    mutationFn: (payload: FeedbackPayload) => TakeoutApi.saveFeedback(payload),
  });
}
