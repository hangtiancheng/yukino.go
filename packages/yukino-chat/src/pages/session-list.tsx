import { customElement } from "@yukino.js/lit-jsx";
import { performLogout } from "@/utils/logout";
import { navigate } from "@/router";
import { TwElement } from "@/styles/base";
import { AppFrame, EmptyPane } from "@/components/app-frame";
import "@/components/session-sidebar";
import { icon, icons } from "@/components/icons";

@customElement("sc-session-list")
export class SessionListPage extends TwElement {
  override render() {
    return (
      <AppFrame
        active="/chat/sessions"
        onLogout={async () => {
          await performLogout();
          navigate("/login");
        }}
        sidebar={
          <x-session-sidebar onChat={(id: string) => navigate(`/chat/${id}`)} />
        }
      >
        <EmptyPane
          iconNode={icon(icons.MessageSquare, "size-7")}
          hint="Select a conversation to start chatting"
        />
      </AppFrame>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sc-session-list": SessionListPage;
  }
}
