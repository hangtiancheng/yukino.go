import { genIdenticon } from "@/lib/avatar";
import { cn } from "@/lib/utils";

interface AvatarProps extends Omit<React.ComponentProps<"img">, "src"> {
  /** Deterministic seed (username, id, ...) for the generated identicon. */
  seed: string;
}

/**
 * Avatar rendered from a runtime-generated identicon data URL
 * (see lib/avatar.ts) — never from an image file or remote URL.
 */
export function Avatar({ seed, className, alt = "", ...props }: AvatarProps) {
  return (
    <img
      src={genIdenticon(seed || "yukino")}
      alt={alt}
      draggable={false}
      className={cn(
        "size-8 shrink-0 rounded-[10px] object-cover select-none",
        className,
      )}
      {...props}
    />
  );
}
