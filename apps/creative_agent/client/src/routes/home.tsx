import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { LogOut, MessageSquare } from "lucide-react";

import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { currentClaims, useAuthStore } from "@/stores/auth";

export function HomePage() {
  const navigate = useNavigate();
  const logout = useAuthStore((s) => s.logout);
  const claims = currentClaims();

  const exampleQuery = useQuery({
    queryKey: ["example"],
    queryFn: () => api.get<unknown>("/api/v1/example"),
  });

  const onLogout = async () => {
    await logout();
    void navigate({ to: "/login" });
  };

  return (
    <div className="bg-muted/40 min-h-svh">
      <div className="mx-auto flex max-w-3xl flex-col gap-6 p-6">
        <header className="flex items-center justify-between">
          <h1 className="text-2xl font-semibold tracking-tight">
            yukino-creative-agent
          </h1>
          <div className="flex items-center gap-2">
            <Button render={<Link to="/conversation" />}>
              <MessageSquare data-icon="inline-start" />
              Chat
            </Button>
            <Button variant="outline" onClick={onLogout}>
              <LogOut data-icon="inline-start" />
              Sign out
            </Button>
          </div>
        </header>

        <Card>
          <CardHeader>
            <CardTitle>Welcome</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-muted-foreground">
              {claims?.username
                ? `Hello, ${claims.username}! Welcome back.`
                : "Hello! Welcome back."}
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Protected endpoint /api/v1/example</CardTitle>
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
                  Failed to load:{" "}
                  {exampleQuery.error instanceof Error
                    ? exampleQuery.error.message
                    : String(exampleQuery.error)}
                </AlertDescription>
              </Alert>
            ) : (
              <pre className="bg-muted overflow-x-auto rounded-lg p-4 text-sm">
                {JSON.stringify(exampleQuery.data, null, 2)}
              </pre>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
