import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowRight, Eraser, LogOut, MessageSquare, Radio } from "lucide-react";
import { motion } from "motion/react";

import { AuroraBackground } from "@/components/common/aurora-background";
import { Avatar } from "@/components/common/avatar";
import { BrandMark } from "@/components/common/brand-mark";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { EASE_OUT_EXPO, fadeUp, staggerGroup } from "@/lib/motion";
import { currentClaims, useAuthStore } from "@/stores/auth";
import { sessionId, useConversationStore } from "@/stores/conversation";

export function HomePage() {
  const navigate = useNavigate();
  const logout = useAuthStore((s) => s.logout);
  const claims = currentClaims();
  const username = claims?.username ?? "creator";

  const messageCount = useConversationStore((s) => s.messages.length);
  const clearMessages = useConversationStore((s) => s.clearMessages);

  const exampleQuery = useQuery({
    queryKey: ["example"],
    queryFn: () => api.get<unknown>("/api/v1/example"),
  });

  const onLogout = async () => {
    await logout();
    void navigate({ to: "/login" });
  };

  return (
    <div className="bg-muted/30 relative min-h-svh overflow-hidden">
      <AuroraBackground intensity="dim" />

      <div className="relative mx-auto flex min-h-svh w-full max-w-6xl flex-col px-4 pb-12 sm:px-6 lg:px-8">
        {/* Top bar */}
        <motion.header
          initial={{ opacity: 0, y: -14 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.5, ease: EASE_OUT_EXPO }}
          className="flex items-center justify-between py-4"
        >
          <div className="flex items-center gap-2.5">
            <BrandMark className="size-8 rounded-xl" />
            <span className="hidden text-sm font-semibold tracking-tight sm:block">
              yukino-creative-agent
            </span>
          </div>
          <div className="flex items-center gap-2">
            <span className="border-border/60 bg-background/70 hidden items-center gap-2 rounded-full border py-1 pr-3 pl-1 backdrop-blur md:flex">
              <Avatar seed={username} className="size-6 rounded-full" />
              <span className="text-xs font-medium">{username}</span>
            </span>
            <Button variant="outline" size="sm" onClick={onLogout}>
              <LogOut data-icon="inline-start" />
              <span className="hidden sm:inline">Sign out</span>
            </Button>
          </div>
        </motion.header>

        {/* Hero + cards */}
        <motion.main
          variants={staggerGroup(0.08, 0.09)}
          initial="hidden"
          animate="show"
          className="flex flex-1 flex-col"
        >
          <section className="pt-10 sm:pt-16 lg:pt-20">
            <motion.div
              variants={fadeUp}
              className="border-border/60 bg-background/60 mb-5 inline-flex items-center gap-2 rounded-full border px-3 py-1 text-xs font-medium backdrop-blur"
            >
              <Radio className="size-3.5 text-violet-500" />
              Multi-agent creative pipeline
            </motion.div>
            <motion.h1
              variants={fadeUp}
              className="max-w-2xl text-3xl font-semibold tracking-tight text-balance sm:text-4xl lg:text-5xl"
            >
              Welcome back,{" "}
              <span className="bg-gradient-to-r from-violet-600 via-fuchsia-500 to-cyan-500 bg-clip-text text-transparent">
                {username}
              </span>
            </motion.h1>
            <motion.p
              variants={fadeUp}
              className="text-muted-foreground mt-4 max-w-xl text-sm leading-relaxed sm:text-base"
            >
              Pick up where you left off, or start a fresh conversation to find
              trending topics, draft a script, and synthesize the final video.
            </motion.p>
            <motion.div
              variants={fadeUp}
              className="mt-7 flex flex-wrap items-center gap-3"
            >
              <Button size="lg" render={<Link to="/conversation" />}>
                <MessageSquare data-icon="inline-start" />
                {messageCount > 0 ? "Continue conversation" : "Start creating"}
                <ArrowRight data-icon="inline-end" />
              </Button>
              {messageCount > 0 ? (
                <Button variant="outline" size="lg" onClick={clearMessages}>
                  <Eraser data-icon="inline-start" />
                  New chat
                </Button>
              ) : null}
            </motion.div>
          </section>

          <section className="mt-12 grid gap-4 md:grid-cols-2 lg:mt-16 lg:grid-cols-3">
            {/* Session card */}
            <motion.div variants={fadeUp} whileHover={{ y: -3 }}>
              <Card className="shadow-foreground/5 h-full shadow-sm">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <MessageSquare className="size-4 text-violet-500" />
                    Current session
                  </CardTitle>
                </CardHeader>
                <CardContent className="flex flex-col gap-3">
                  <p className="text-muted-foreground text-sm">
                    {messageCount > 0
                      ? `${messageCount} message${messageCount === 1 ? "" : "s"} in this tab's conversation.`
                      : "No messages yet — the conversation starts empty."}
                  </p>
                  <p className="bg-muted/70 text-muted-foreground truncate rounded-lg px-2.5 py-1.5 font-mono text-xs">
                    sid: {sessionId()}
                  </p>
                </CardContent>
              </Card>
            </motion.div>

            {/* Pipeline card */}
            <motion.div variants={fadeUp} whileHover={{ y: -3 }}>
              <Card className="shadow-foreground/5 h-full shadow-sm">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <Radio className="size-4 text-cyan-500" />
                    How it works
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <ol className="text-muted-foreground flex flex-col gap-2.5 text-sm">
                    {[
                      "Browse hot topics and pick a direction",
                      "Review and confirm the generated script",
                      "The pipeline synthesizes the final video",
                    ].map((step, i) => (
                      <li key={step} className="flex items-start gap-2.5">
                        <span className="bg-primary text-primary-foreground mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full text-[0.7rem] font-semibold">
                          {i + 1}
                        </span>
                        {step}
                      </li>
                    ))}
                  </ol>
                </CardContent>
              </Card>
            </motion.div>

            {/* Protected endpoint card */}
            <motion.div variants={fadeUp} whileHover={{ y: -3 }}>
              <Card className="shadow-foreground/5 h-full shadow-sm">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <span className="relative flex size-2.5">
                      <span
                        className={`absolute inline-flex size-full rounded-full opacity-60 ${
                          exampleQuery.isPending
                            ? "bg-amber-400"
                            : exampleQuery.isError
                              ? "bg-destructive"
                              : "bg-emerald-400"
                        }`}
                      />
                      <span
                        className={`relative inline-flex size-2.5 rounded-full ${
                          exampleQuery.isPending
                            ? "bg-amber-400"
                            : exampleQuery.isError
                              ? "bg-destructive"
                              : "bg-emerald-400"
                        }`}
                      />
                    </span>
                    API status
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  {exampleQuery.isPending ? (
                    <div className="flex flex-col gap-2">
                      <Skeleton className="h-4 w-full" />
                      <Skeleton className="h-4 w-2/3" />
                    </div>
                  ) : exampleQuery.isError ? (
                    <Alert variant="destructive">
                      <AlertDescription>
                        /api/v1/example failed:{" "}
                        {exampleQuery.error instanceof Error
                          ? exampleQuery.error.message
                          : String(exampleQuery.error)}
                      </AlertDescription>
                    </Alert>
                  ) : (
                    <div className="flex flex-col gap-2">
                      <p className="text-muted-foreground text-sm">
                        <span className="font-medium text-emerald-600">
                          Online
                        </span>{" "}
                        — protected endpoint responded.
                      </p>
                      <pre className="bg-muted/70 max-h-28 overflow-auto rounded-lg p-3 text-xs">
                        {JSON.stringify(exampleQuery.data, null, 2)}
                      </pre>
                    </div>
                  )}
                </CardContent>
              </Card>
            </motion.div>
          </section>
        </motion.main>
      </div>
    </div>
  );
}
