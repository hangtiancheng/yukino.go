import { cn } from "@/lib/utils";

interface TableProps {
  className?: string;
  children?: unknown;
}

export function Table({ className, children }: TableProps) {
  return (
    <div className="w-full overflow-x-auto">
      <table className={cn("text-foreground w-full caption-bottom", className)}>
        {children}
      </table>
    </div>
  );
}

export function TableHeader({ children }: TableProps) {
  return <thead>{children}</thead>;
}

export function TableBody({ children }: TableProps) {
  return <tbody>{children}</tbody>;
}

export function TableRow({
  className,
  children,
}: {
  className?: string;
  children?: unknown;
}) {
  return (
    <tr
      className={cn(
        "border-border hover:bg-accent/50 border-b transition-colors",
        className,
      )}
    >
      {children}
    </tr>
  );
}

export function TableHead({
  className,
  children,
}: {
  className?: string;
  children?: unknown;
}) {
  return (
    <th
      className={cn(
        "text-muted-foreground h-10 px-3 text-left align-middle text-xs font-semibold tracking-wide",
        className,
      )}
    >
      {children}
    </th>
  );
}

export function TableCell({
  className,
  children,
}: {
  className?: string;
  children?: unknown;
}) {
  return (
    <td className={cn("px-3 py-2.5 align-middle text-sm", className)}>
      {children}
    </td>
  );
}
