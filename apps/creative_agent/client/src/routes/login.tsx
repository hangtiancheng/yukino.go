import { Link, useNavigate } from "@tanstack/react-router";
import { LogIn } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useState } from "react";

import { AuthLayout } from "@/components/common/auth-layout";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
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
  useEffect(() => {
    if (token) void navigate({ to: "/", replace: true });
  }, [token, navigate]);

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
    <AuthLayout
      title="Sign in"
      description="yukino-creative-agent conversational agent system"
      footer={
        <>
          No account yet?{" "}
          <Link
            to="/register"
            className="text-primary font-medium underline-offset-4 hover:underline"
          >
            Register now
          </Link>
        </>
      }
    >
      <AnimatePresence>
        {error ? (
          <motion.div
            key={error}
            initial={{ opacity: 0, x: [0, -8, 8, -5, 5, 0] }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.4 }}
            className="mb-4"
          >
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          </motion.div>
        ) : null}
      </AnimatePresence>
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
          <motion.div whileTap={{ scale: pending ? 1 : 0.98 }}>
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
          </motion.div>
        </FieldGroup>
      </form>
    </AuthLayout>
  );
}
