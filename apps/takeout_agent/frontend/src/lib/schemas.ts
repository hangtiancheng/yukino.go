import { z } from "zod";

export const sourceModeSchema = z.enum(["PERSONAL", "PUBLIC"]);
export type SourceMode = z.infer<typeof sourceModeSchema>;

export const intentSchema = z.enum([
  "MEAL_RECOMMENDATION",
  "CLARIFY_NEEDED",
  "MEAL_ADJUST",
  "MEAL_PLAN",
  "HEALTH_RISK",
  "OTHER",
]);
export type Intent = z.infer<typeof intentSchema>;

export const clarifyActionSchema = z.enum(["ASK", "READY"]);
export type ClarifyAction = z.infer<typeof clarifyActionSchema>;

export const mealResponseSchema = z.object({
  id: z.number(),
  sourceType: sourceModeSchema,
  name: z.string(),
  mealTime: z.array(z.string()),
  mood: z.array(z.string()),
  scene: z.array(z.string()),
  healthGoal: z.array(z.string()),
  cuisine: z.array(z.string()),
  taste: z.array(z.string()),
  convenience: z.array(z.string()),
  matchScore: z.number(),
});
export type MealResponse = z.infer<typeof mealResponseSchema>;

export const chatResponseSchema = z.object({
  sessionId: z.string(),
  traceId: z.string().optional(),
  responseType: z.string(),
  speechText: z.string(),
  displayBlocks: z.array(mealResponseSchema),
  nextAction: z.string(),
  clarifyQuestion: z.string().optional(),
  missingSlots: z.array(z.string()),
});
export type ChatResponse = z.infer<typeof chatResponseSchema>;

export const createSessionResponseSchema = z.object({
  sessionId: z.string(),
});

export const slotOptionsSchema = z.record(z.string(), z.array(z.string()));
export type SlotOptions = z.infer<typeof slotOptionsSchema>;

export const traceRowSchema = z.object({
  id: z.number(),
  traceId: z.string(),
  sessionId: z.string(),
  userId: z.number(),
  status: z.string(),
  eventCount: z.number(),
  durationMs: z.number().nullable().optional(),
  errorMessage: z.string().nullable().optional(),
  traceJson: z.string(),
  expectedIntent: z.string().nullable().optional(),
  expectedSlots: z.string().nullable().optional(),
  expectedClarifyAction: z.string().nullable().optional(),
  labeledBy: z.number().nullable().optional(),
  labeledAt: z.string().nullable().optional(),
  labelNote: z.string().nullable().optional(),
  createdAt: z.string(),
  updatedAt: z.string(),
});
export type TraceRow = z.infer<typeof traceRowSchema>;

export const traceEvaluationResultSchema = z.object({
  traceId: z.string(),
  sessionId: z.string(),
  createdAt: z.string().nullable().optional(),
  score: z.number().nullable().optional(),
  ruleScore: z.number().nullable().optional(),
  llmJudgeScore: z.number().nullable().optional(),
  userFeedbackScore: z.number().nullable().optional(),
  metrics: z.record(z.string(), z.number().nullable().optional()),
  detail: z.record(z.string(), z.unknown()),
});
export type TraceEvaluationResult = z.infer<typeof traceEvaluationResultSchema>;

export const evaluationReportSchema = z.object({
  startAt: z.string(),
  endAt: z.string(),
  totalTraces: z.number(),
  labeledTraces: z.number(),
  avgScore: z.number().nullable().optional(),
  metricAverages: z.record(z.string(), z.number().nullable().optional()),
  traceResults: z.array(traceEvaluationResultSchema),
});
export type EvaluationReport = z.infer<typeof evaluationReportSchema>;

export const mealRequestSchema = z.object({
  name: z.string().trim().min(1, "Meal name is required"),
  mealTime: z.array(z.string()).min(1, "Pick at least one meal time"),
  mood: z.array(z.string()),
  scene: z.array(z.string()),
  healthGoal: z.array(z.string()),
  cuisine: z.array(z.string()),
  taste: z.array(z.string()),
  convenience: z.array(z.string()),
});
export type MealRequest = z.infer<typeof mealRequestSchema>;
