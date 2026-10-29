import type { Transition, Variants } from "motion/react";

/** Shared motion presets so every surface animates with the same feel. */

export const EASE_OUT_EXPO: [number, number, number, number] = [
  0.16, 1, 0.3, 1,
];

export const SPRING_SMOOTH: Transition = {
  type: "spring",
  stiffness: 380,
  damping: 32,
  mass: 0.9,
};

export const SPRING_GENTLE: Transition = {
  type: "spring",
  stiffness: 260,
  damping: 26,
};

export const fadeUp: Variants = {
  hidden: { opacity: 0, y: 18 },
  show: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.55, ease: EASE_OUT_EXPO },
  },
};

export const fadeIn: Variants = {
  hidden: { opacity: 0 },
  show: { opacity: 1, transition: { duration: 0.4, ease: EASE_OUT_EXPO } },
};

export const popIn: Variants = {
  hidden: { opacity: 0, scale: 0.95, y: 10 },
  show: { opacity: 1, scale: 1, y: 0, transition: SPRING_SMOOTH },
};

/** Parent variant that staggers its children's `hidden -> show` transitions. */
export function staggerGroup(
  delayChildren = 0,
  staggerChildren = 0.07,
): Variants {
  return {
    hidden: {},
    show: { transition: { delayChildren, staggerChildren } },
  };
}
