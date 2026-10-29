import { motion } from "motion/react";
import { Clapperboard, Sparkles, Wand2 } from "lucide-react";

import {
  AuroraBackground,
  GridOverlay,
} from "@/components/common/aurora-background";
import { BrandMark } from "@/components/common/brand-mark";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { SPRING_GENTLE, fadeUp, staggerGroup } from "@/lib/motion";

const FEATURES = [
  {
    icon: Sparkles,
    title: "Hot-topic radar",
    text: "Surface trending ideas worth creating on, in real time.",
  },
  {
    icon: Wand2,
    title: "Script generation",
    text: "Turn a topic into a structured, ready-to-shoot script.",
  },
  {
    icon: Clapperboard,
    title: "Video synthesis",
    text: "Dispatch the pipeline and watch the final cut come together.",
  },
];

interface AuthLayoutProps {
  title: string;
  description: string;
  children: React.ReactNode;
  footer: React.ReactNode;
}

/**
 * Split-screen auth shell:
 * - desktop (lg+): animated dark brand panel on the left, form on the right;
 * - mobile: compact brand header + centered card over a soft aurora.
 */
export function AuthLayout({
  title,
  description,
  children,
  footer,
}: AuthLayoutProps) {
  return (
    <div className="relative min-h-svh lg:grid lg:grid-cols-[1.05fr_1fr]">
      {/* Mobile-only soft background */}
      <AuroraBackground intensity="dim" className="lg:hidden" />

      {/* Desktop brand panel */}
      <motion.aside
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.6 }}
        className="relative hidden overflow-hidden bg-[#0c0815] lg:flex lg:flex-col"
      >
        <AuroraBackground />
        <GridOverlay />
        <div className="relative flex h-full flex-col p-10 xl:p-14">
          <motion.div
            variants={staggerGroup(0.05, 0.08)}
            initial="hidden"
            animate="show"
            className="flex items-center gap-3"
          >
            <motion.div variants={fadeUp}>
              <BrandMark className="size-10 rounded-2xl" />
            </motion.div>
            <motion.span
              variants={fadeUp}
              className="text-lg font-semibold tracking-tight text-white"
            >
              yukino-creative-agent
            </motion.span>
          </motion.div>

          <motion.div
            variants={staggerGroup(0.15, 0.09)}
            initial="hidden"
            animate="show"
            className="mt-auto max-w-md"
          >
            <motion.h2
              variants={fadeUp}
              className="text-4xl leading-tight font-semibold tracking-tight text-white xl:text-[2.75rem]"
            >
              From trend to final cut,{" "}
              <span className="bg-gradient-to-r from-violet-400 via-fuchsia-300 to-cyan-300 bg-clip-text text-transparent">
                one conversation
              </span>
              .
            </motion.h2>
            <motion.p
              variants={fadeUp}
              className="mt-4 text-sm leading-relaxed text-white/60"
            >
              A multi-agent creative studio: it watches what is trending, writes
              the script with you, and dispatches video synthesis — all inside a
              single chat.
            </motion.p>

            <motion.ul
              variants={staggerGroup(0.1, 0.1)}
              className="mt-10 space-y-5"
            >
              {FEATURES.map((f) => (
                <motion.li
                  key={f.title}
                  variants={fadeUp}
                  className="flex items-start gap-3.5"
                >
                  <span className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-xl border border-white/10 bg-white/5 text-violet-300 backdrop-blur">
                    <f.icon className="size-4.5" />
                  </span>
                  <span>
                    <span className="block text-sm font-medium text-white">
                      {f.title}
                    </span>
                    <span className="mt-0.5 block text-xs leading-relaxed text-white/50">
                      {f.text}
                    </span>
                  </span>
                </motion.li>
              ))}
            </motion.ul>
          </motion.div>

          <motion.p
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ delay: 0.7, duration: 0.6 }}
            className="relative mt-10 text-xs text-white/35"
          >
            Powered by the yukino multi-agent pipeline
          </motion.p>
        </div>
      </motion.aside>

      {/* Form side */}
      <div className="relative flex min-h-svh flex-col items-center justify-center px-4 py-10 sm:px-8">
        {/* Mobile brand header */}
        <motion.div
          initial={{ opacity: 0, y: -12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={SPRING_GENTLE}
          className="mb-6 flex items-center gap-2.5 lg:hidden"
        >
          <BrandMark className="size-9 rounded-xl" />
          <span className="text-base font-semibold tracking-tight">
            yukino-creative-agent
          </span>
        </motion.div>

        <motion.div
          initial={{ opacity: 0, y: 24, scale: 0.98 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          transition={SPRING_GENTLE}
          className="w-full max-w-sm"
        >
          <Card className="shadow-foreground/5 shadow-lg">
            <CardHeader>
              <CardTitle className="text-xl">{title}</CardTitle>
              <CardDescription>{description}</CardDescription>
            </CardHeader>
            <CardContent>
              {children}
              <div className="text-muted-foreground mt-4 text-center text-sm">
                {footer}
              </div>
            </CardContent>
          </Card>
        </motion.div>
      </div>
    </div>
  );
}
