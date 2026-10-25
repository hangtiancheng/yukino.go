import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  DietApi,
  type EvaluationPayload,
  type FeedbackPayload,
  type TraceLabelPayload,
  type TraceListParams,
} from "@/lib/api";
import type { MealRequest } from "@/lib/schemas";

export function useSlotOptions() {
  return useQuery({
    queryKey: ["slot-options"],
    queryFn: DietApi.slotOptions,
    staleTime: 5 * 60 * 1000,
  });
}

export function usePersonalMeals() {
  return useQuery({
    queryKey: ["meals", "personal"],
    queryFn: DietApi.listPersonalMeals,
  });
}

export function usePublicMeals() {
  return useQuery({
    queryKey: ["meals", "public"],
    queryFn: DietApi.listPublicMeals,
  });
}

export function useCreateMeal() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: MealRequest) => DietApi.createPersonalMeal(payload),
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
    }) => DietApi.updatePersonalMeal(mealId, payload),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["meals", "personal"] }),
  });
}

export function useDeleteMeal() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (mealId: number) => DietApi.deletePersonalMeal(mealId),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["meals", "personal"] }),
  });
}

export function useTraces(params: TraceListParams) {
  return useQuery({
    queryKey: ["traces", "list", params],
    queryFn: () => DietApi.listTraces(params),
  });
}

export function useTraceDetail(traceId: string | null) {
  return useQuery({
    queryKey: ["traces", "detail", traceId],
    queryFn: () => DietApi.getTrace(traceId as string),
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
    }) => DietApi.labelTrace(traceId, payload),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["traces"] }),
  });
}

export function useEvaluate() {
  return useMutation({
    mutationFn: (payload: EvaluationPayload) => DietApi.evaluate(payload),
  });
}

export function useSaveFeedback() {
  return useMutation({
    mutationFn: (payload: FeedbackPayload) => DietApi.saveFeedback(payload),
  });
}
