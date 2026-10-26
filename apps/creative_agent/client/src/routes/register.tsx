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
          <CardTitle className="text-xl">注册</CardTitle>
          <CardDescription>创建新账号</CardDescription>
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
                注册成功！{" "}
                <Link
                  to="/login"
                  className="font-medium underline-offset-4 hover:underline"
                >
                  点击这里去登录
                </Link>
              </AlertDescription>
            </Alert>
          ) : null}
          <form onSubmit={onSubmit}>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="reg-username">用户名</FieldLabel>
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
                <FieldLabel htmlFor="reg-password">密码</FieldLabel>
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
                <FieldLabel htmlFor="reg-email">邮箱</FieldLabel>
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
                    注册中...
                  </>
                ) : (
                  <>
                    <UserPlus data-icon="inline-start" />
                    注册
                  </>
                )}
              </Button>
            </FieldGroup>
          </form>
          <p className="text-muted-foreground mt-4 text-center text-sm">
            已有账号？{" "}
            <Link
              to="/login"
              className="text-primary font-medium underline-offset-4 hover:underline"
            >
              去登录
            </Link>
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
