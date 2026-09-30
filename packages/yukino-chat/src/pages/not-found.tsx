import { customElement } from "@yukino.js/lit-jsx";
import { navigate } from "@/router";
import { TwElement } from "@/styles/base";
import { Button } from "@/components/ui/button";

@customElement("sc-not-found")
export class NotFoundPage extends TwElement {
  override render() {
    return (
      <div className="bg-background relative flex min-h-screen items-center justify-center overflow-hidden p-4">
        <div
          aria-hidden="true"
          className="bg-primary/20 pointer-events-none absolute -top-24 right-1/4 size-80 rounded-full blur-3xl"
        />
        <div
          aria-hidden="true"
          className="bg-primary/35 pointer-events-none absolute -bottom-32 left-1/4 size-96 rounded-full blur-3xl"
        />

        <div className="animate-in fade-in zoom-in-95 text-center duration-300">
          <h1 className="text-primary/30 text-8xl font-bold tracking-tight">
            404
          </h1>
          <p className="text-muted-foreground mt-4">Page not found</p>
          <Button className="mt-8" onClick={() => navigate("/login")}>
            Back to Home
          </Button>
        </div>
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sc-not-found": NotFoundPage;
  }
}
