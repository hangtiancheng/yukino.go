import { Link, useNavigate } from "@tanstack/react-router";
import { UserPlus } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useState } from "react";

import { AuthLayout } from "@/components/common/auth-layout";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { useAuthStore } from "@/stores/auth";

export function RegisterPage() {
  const navigate = useNavigate();
  const register = useAuthStore((s) => s.register);
  const token = useAuthStore((s) => s.token);

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [email, setEmail] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [pending, setPending] = useState(false);

  // Already signed in: go straight to the home page
  useEffect(() => {
    if (token) void navigate({ to: "/", replace: true });
  }, [token, navigate]);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccess(false);
    setPending(true);
    try {
      await register(username, password, email);
      setSuccess(true);
      setUsername("");
      setPassword("");
      setEmail("");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  };

  return (
    <AuthLayout
      title="Register"
      description="Create a new account"
      footer={
        <>
          Already have an account?{" "}
          <Link
            to="/login"
            className="text-primary font-medium underline-offset-4 hover:underline"
          >
            Sign in
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
      <AnimatePresence>
        {success ? (
          <motion.div
            initial={{ opacity: 0, y: -8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            className="mb-4"
          >
            <Alert>
              <AlertDescription>
                Registered successfully!{" "}
                <Link
                  to="/login"
                  className="font-medium underline-offset-4 hover:underline"
                >
                  Click here to sign in
                </Link>
              </AlertDescription>
            </Alert>
          </motion.div>
        ) : null}
      </AnimatePresence>
      <form onSubmit={onSubmit}>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="reg-username">Username</FieldLabel>
            <Input
              id="reg-username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              minLength={3}
              maxLength={64}
              autoComplete="username"
              required
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="reg-password">Password</FieldLabel>
            <Input
              id="reg-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              minLength={6}
              autoComplete="new-password"
              required
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="reg-email">Email</FieldLabel>
            <Input
              id="reg-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="email"
              required
            />
          </Field>
          <motion.div whileTap={{ scale: pending ? 1 : 0.98 }}>
            <Button type="submit" disabled={pending} className="w-full">
              {pending ? (
                <>
                  <Spinner data-icon="inline-start" />
                  Registering...
                </>
              ) : (
                <>
                  <UserPlus data-icon="inline-start" />
                  Register
                </>
              )}
            </Button>
          </motion.div>
        </FieldGroup>
      </form>
    </AuthLayout>
  );
}
