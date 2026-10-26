import { useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  ArrowRight,
  ClipboardCheck,
  Globe,
  MessagesSquare,
  Route,
  Salad,
  SearchCheck,
  UtensilsCrossed,
} from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router-dom";

import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { usePersonalMeals, usePublicMeals, useTraces } from "@/hooks/queries";
import { defaultRange } from "@/lib/format";

const QUICK_PROMPTS = [
  "Something quick and light for breakfast",
  "Low-fat dinner ideas for tonight",
  "Feeling down, want something warm and comforting",
  "My stomach hurts, what should I eat?",
];

const PIPELINE = [
  {
    icon: SearchCheck,
    title: "Understand intent",
    description:
      "The intent agent classifies each message into recommendation, clarify, plan, risk, and more.",
  },
  {
    icon: Route,
    title: "Fill the slots",
    description:
      "Meal time, mood, scene, health goal, cuisine, taste, and convenience are extracted from your words.",
  },
  {
    icon: Salad,
    title: "Rank and recommend",
    description:
      "Candidates from your personal or the public library are scored against the slot bundle.",
  },
  {
    icon: ClipboardCheck,
    title: "Learn from feedback",
    description:
      "Likes, adoptions, and dislikes feed the evaluation pipeline with rule and LLM-judge scores.",
  },
];

export default function DashboardPage() {
  const personalMeals = usePersonalMeals();
  const publicMeals = usePublicMeals();
  const queryClient = useQueryClient();
  const range = useMemo(() => defaultRange(), []);
  const traces = useTraces({
    startAt: range.startAt,
    endAt: range.endAt,
    limit: 500,
  });

  const startChatWith = (prompt: string) => {
    sessionStorage.setItem("takeout.pendingPrompt", prompt);
  };

  return (
    <div className="mx-auto max-w-5xl space-y-6">
      <section className="from-primary/15 via-background to-background relative overflow-hidden rounded-2xl border bg-gradient-to-br p-6 md:p-10">
        <div className="bg-primary/10 pointer-events-none absolute -top-10 -right-10 size-48 rounded-full blur-2xl" />
        <div className="relative max-w-2xl space-y-4">
          <span className="bg-background/70 text-muted-foreground inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium">
            <Salad className="text-primary size-3.5" />
            Multi-agent takeout recommendation
          </span>
          <h2 className="text-2xl font-bold tracking-tight md:text-3xl">
            Tell me how you feel, and I will pick what you should eat.
          </h2>
          <p className="text-muted-foreground text-sm md:text-base">
            A conversational agent that understands your meal time, mood, scene,
            health goals, and taste, then recommends dishes from your personal
            or the public meal library.
          </p>
          <div className="flex flex-wrap gap-2">
            <Button size="lg" render={<Link to="/chat" />} nativeButton={false}>
              <MessagesSquare />
              Start chatting
            </Button>
            <Button
              variant="outline"
              size="lg"
              render={<Link to="/meals/personal" />}
              nativeButton={false}
            >
              <UtensilsCrossed />
              Manage my meals
            </Button>
          </div>
        </div>
      </section>

      <section className="grid gap-4 sm:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="flex items-center gap-1.5">
              <span className="bg-primary/10 text-primary flex size-6 items-center justify-center rounded-md">
                <UtensilsCrossed className="size-3.5" />
              </span>
              Personal library
            </CardDescription>
            <CardTitle className="text-3xl tabular-nums">
              {personalMeals.isPending ? (
                <Skeleton className="h-9 w-16" />
              ) : (
                (personalMeals.data?.length ?? 0)
              )}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <Button
              variant="link"
              size="sm"
              className="-ml-2 h-auto p-0"
              render={<Link to="/meals/personal" />}
              nativeButton={false}
            >
              Browse personal meals <ArrowRight className="size-3.5" />
            </Button>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="flex items-center gap-1.5">
              <span className="flex size-6 items-center justify-center rounded-md bg-sky-500/10 text-sky-600 dark:text-sky-400">
                <Globe className="size-3.5" />
              </span>
              Public library
            </CardDescription>
            <CardTitle className="text-3xl tabular-nums">
              {publicMeals.isPending ? (
                <Skeleton className="h-9 w-16" />
              ) : (
                (publicMeals.data?.length ?? 0)
              )}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <Button
              variant="link"
              size="sm"
              className="-ml-2 h-auto p-0"
              render={<Link to="/meals/public" />}
              nativeButton={false}
            >
              Browse public meals <ArrowRight className="size-3.5" />
            </Button>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="flex items-center gap-1.5">
              <span className="flex size-6 items-center justify-center rounded-md bg-violet-500/10 text-violet-600 dark:text-violet-400">
                <Activity className="size-3.5" />
              </span>
              Traces (last 24h)
            </CardDescription>
            <CardTitle className="text-3xl tabular-nums">
              {traces.isPending ? (
                <Skeleton className="h-9 w-16" />
              ) : (
                (traces.data?.length ?? 0)
              )}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <Button
              variant="link"
              size="sm"
              className="-ml-2 h-auto p-0"
              render={<Link to="/traces" />}
              nativeButton={false}
            >
              Inspect request traces <ArrowRight className="size-3.5" />
            </Button>
          </CardContent>
        </Card>
      </section>

      <section className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>How it works</CardTitle>
            <CardDescription>
              Four stages run on every chat turn, fully traced.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {PIPELINE.map((step, index) => (
              <div key={step.title} className="flex gap-3">
                <span className="bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg">
                  <step.icon className="size-4.5" />
                </span>
                <div className="min-w-0">
                  <p className="text-sm font-medium">
                    {index + 1}. {step.title}
                  </p>
                  <p className="text-muted-foreground text-sm">
                    {step.description}
                  </p>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Try a quick prompt</CardTitle>
            <CardDescription>
              Jump straight into the chat with one of these.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            {QUICK_PROMPTS.map((prompt) => (
              <Button
                key={prompt}
                variant="secondary"
                className="h-auto w-full justify-start px-3 py-2 text-left whitespace-normal"
                render={
                  <Link to="/chat" onClick={() => startChatWith(prompt)} />
                }
                nativeButton={false}
              >
                {prompt}
              </Button>
            ))}
            <p className="text-muted-foreground pt-1 text-xs">
              Tip: switch between the personal and public library from the chat
              toolbar.
            </p>
          </CardContent>
        </Card>
      </section>

      <section className="bg-muted/40 text-muted-foreground rounded-xl border p-4 text-sm">
        <div className="flex flex-wrap items-center gap-2">
          <Globe className="text-primary size-4" />
          <span>
            Backend runs on yukino_http with a MySQL-backed store. Data changes
            refresh automatically through TanStack Query.
          </span>
          <button
            type="button"
            className="text-primary ml-auto underline-offset-2 hover:underline"
            onClick={() => void queryClient.invalidateQueries()}
          >
            Refresh all data
          </button>
        </div>
      </section>
    </div>
  );
}
