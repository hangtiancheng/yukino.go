import { customElement } from "@yukino.js/lit-jsx";
import { performLogout } from "@/utils/logout";
import { navigate } from "@/router";
import { TwElement } from "@/styles/base";
import { AppFrame, EmptyPane } from "@/components/app-frame";
import "@/components/contact-sidebar";
import { icon, icons } from "@/components/icons";

@customElement("sc-contact-list")
export class ContactListPage extends TwElement {
  override render() {
    return (
      <AppFrame
        active="/chat/contacts"
        onLogout={async () => {
          await performLogout();
          navigate("/login");
        }}
        sidebar={
          <x-contact-sidebar
            onNavigate={(id: string) => navigate(`/chat/${id}`)}
          />
        }
      >
        <EmptyPane
          iconNode={icon(icons.User, "size-7")}
          hint="Select a contact to start chatting"
        />
      </AppFrame>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sc-contact-list": ContactListPage;
  }
}
