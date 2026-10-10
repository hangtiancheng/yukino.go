import { z } from "zod/v4";

export const chatResponseSchema = z.object({
  message: z.string(),
  data: z
    .object({
      answer: z.string(),
    })
    .nullable(),
});

export const aiOpsResponseSchema = z.object({
  message: z.string(),
  data: z
    .object({
      result: z.string(),
      detail: z.array(z.string()).optional(),
    })
    .nullable(),
});

export const uploadResponseSchema = z.object({
  message: z.string(),
  data: z.unknown().optional(),
});
