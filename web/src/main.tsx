import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App";
import { LanguageProvider } from "./i18n";
import { RouterProvider } from "./router";
import { SessionProvider } from "./session";
import "./styles/index.css";

const container = document.getElementById("root");
if (!container) {
  throw new Error("No #root element to mount into");
}

createRoot(container).render(
  <StrictMode>
    <LanguageProvider>
      <RouterProvider>
        <SessionProvider>
          <App />
        </SessionProvider>
      </RouterProvider>
    </LanguageProvider>
  </StrictMode>,
);
