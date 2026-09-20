import { z } from "zod/v4";

// Unified API response shape { message, data }, used by the chat store to
// parse fetch responses with zod instead of type assertions. Mirrors the
// shapes returned by the Go backend route handlers. The backend always
// serializes the data key — null on error responses — so nullable, not optional.

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
