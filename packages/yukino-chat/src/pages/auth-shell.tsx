import { icon, icons } from "@/components/icons";
import {
  Card,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

/** Centered auth card over ambient peach blobs. */
export function AuthShell({
  title,
  description,
  children,
  footer,
}: {
  title: string;
  description: string;
  children?: unknown;
  footer?: unknown;
}) {
  return (
    <div className="bg-background relative flex min-h-screen items-center justify-center overflow-hidden p-4">
      <div
        aria-hidden="true"
        className="bg-primary/40 pointer-events-none absolute -top-32 -left-32 size-96 animate-pulse rounded-full blur-3xl [animation-duration:7s] motion-reduce:animate-none"
      />
      <div
        aria-hidden="true"
        className="bg-primary/25 pointer-events-none absolute -right-24 -bottom-40 h-112 w-96 animate-pulse rounded-full blur-3xl [animation-delay:1.5s] [animation-duration:9s] motion-reduce:animate-none"
      />
      <div
        aria-hidden="true"
        className="bg-primary-deep/10 pointer-events-none absolute top-1/4 right-1/3 size-64 rounded-full blur-3xl"
      />

      <Card className="animate-in fade-in zoom-in-95 w-full max-w-md duration-300">
        <CardHeader>
          <div className="mb-2 flex items-center gap-2.5">
            <span className="bg-primary text-primary-foreground flex size-9 items-center justify-center rounded-xl shadow-sm">
              {icon(icons.MessageCircle, "size-5")}
            </span>
            <span className="text-foreground text-sm font-semibold tracking-tight">
              Yukino Chat
            </span>
          </div>
          <CardTitle>{title}</CardTitle>
          <CardDescription>{description}</CardDescription>
        </CardHeader>
        <div className="flex flex-col gap-4 px-6 pb-4">{children}</div>
        <CardFooter className="flex-col gap-3">{footer}</CardFooter>
      </Card>
    </div>
  );
}
