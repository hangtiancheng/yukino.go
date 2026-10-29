import { useId } from "react";

import { cn } from "@/lib/utils";

/**
 * Inline-SVG brand mark (a violet -> cyan bolt). Code-native asset: the app
 * ships zero image files; the favicon is this same mark as a data URI.
 */
export function BrandMark({ className }: { className?: string }) {
  const rawId = useId();
  const gradId = `brand-grad-${rawId.replace(/[^a-zA-Z0-9]/g, "")}`;
  return (
    <svg
      viewBox="0 0 48 48"
      fill="none"
      aria-hidden="true"
      className={cn("size-8 shrink-0", className)}
    >
      <defs>
        <linearGradient
          id={gradId}
          x1="4"
          y1="2"
          x2="44"
          y2="46"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="#8b5cf6" />
          <stop offset="0.55" stopColor="#7e14ff" />
          <stop offset="1" stopColor="#47bfff" />
        </linearGradient>
      </defs>
      <rect width="48" height="48" rx="13" fill={`url(#${gradId})`} />
      <path d="M27.4 8 14 27.2h8.6L20 40l14-19.6h-9.1L27.4 8Z" fill="#fff" />
    </svg>
  );
}
