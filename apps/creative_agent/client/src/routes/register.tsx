import { Link } from "@tanstack/react-router";
import { UserPlus } from "lucide-react";
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

export function RegisterPage() {
  const register = useAuthStore((s) => s.register);

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [email, setEmail] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [pending, setPending] = useState(false);

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
    <div className="bg-muted/40 flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">Register</CardTitle>
          <CardDescription>Create a new account</CardDescription>
        </CardHeader>
        <CardContent>
          {error ? (
            <Alert variant="destructive" className="mb-4">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
          {success ? (
            <Alert className="mb-4">
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
          ) : null}
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
            </FieldGroup>
          </form>
          <p className="text-muted-foreground mt-4 text-center text-sm">
            Already have an account?{" "}
            <Link
              to="/login"
              className="text-primary font-medium underline-offset-4 hover:underline"
            >
              Sign in
            </Link>
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
