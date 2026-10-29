import { Link, useNavigate } from "@tanstack/react-router";
import { LogIn } from "lucide-react";
import { useState } from "react";

import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { useAuthStore } from "@/stores/auth";

export function LoginPage() {
  const navigate = useNavigate();
  const login = useAuthStore((s) => s.login);
  const token = useAuthStore((s) => s.token);

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  // Already signed in: go straight to the home page
  if (token) {
    void navigate({ to: "/", replace: true });
  }

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setPending(true);
    try {
      await login(username, password);
      void navigate({ to: "/" });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  };

  return (
    <div className="bg-muted/40 flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">Sign in</CardTitle>
          <CardDescription>
            yukino-creative-agent conversational agent system
          </CardDescription>
        </CardHeader>
        <CardContent>
          {error ? (
            <Alert variant="destructive" className="mb-4">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
          <form onSubmit={onSubmit}>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="username">Username</FieldLabel>
                <Input
                  id="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="username"
                  required
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="password">Password</FieldLabel>
                <Input
                  id="password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="current-password"
                  required
                />
              </Field>
              <Button type="submit" disabled={pending} className="w-full">
                {pending ? (
                  <>
                    <Spinner data-icon="inline-start" />
                    Signing in...
                  </>
                ) : (
                  <>
                    <LogIn data-icon="inline-start" />
                    Sign in
                  </>
                )}
              </Button>
            </FieldGroup>
          </form>
          <p className="text-muted-foreground mt-4 text-center text-sm">
            No account yet?{" "}
            <Link
              to="/register"
              className="text-primary font-medium underline-offset-4 hover:underline"
            >
              Register now
            </Link>
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
