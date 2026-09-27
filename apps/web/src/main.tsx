import React from "react";
import {createRoot} from "react-dom/client";
import {BrowserRouter} from "react-router-dom";
import {App} from "./App";
import {ErrorBoundary} from "./components/ErrorBoundary";
import {SessionProvider} from "./providers/SessionProvider";
import {ToastProvider} from "./providers/ToastProvider";
import "./styles.css";

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <BrowserRouter>
        <SessionProvider>
          <ToastProvider>
            <App/>
          </ToastProvider>
        </SessionProvider>
      </BrowserRouter>
    </ErrorBoundary>
  </React.StrictMode>,
);
