import "./index.css";
import "./app-root";
import { ensureToaster } from "./components/toaster";

ensureToaster();

document
  .getElementById("root")!
  .appendChild(document.createElement("yukino-app"));
