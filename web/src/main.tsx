import { QueryClientProvider } from "@tanstack/react-query";
import { StrictMode, Suspense } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Route, Routes } from "react-router";
import { Toaster } from "sonner";
import { AppShell } from "./components/AppShell";
import { LanguageRoot } from "./components/LanguageRoot";
import { initI18n } from "./i18n";
import "./index.css";
import { queryClient } from "./lib/queries";
import { Login, Settings, WarrantyDetail } from "./pages/lazy";
import { Warranties } from "./pages/Warranties";

const root = document.getElementById("root");
if (!root) throw new Error("#root element missing");

await initI18n();

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <LanguageRoot>
          <Suspense fallback={null}>
            <Routes>
              <Route path="/login" element={<Login />} />
              <Route
                path="/"
                element={
                  <AppShell>
                    <Warranties />
                  </AppShell>
                }
              />
              <Route
                path="/w/:id"
                element={
                  <AppShell>
                    <WarrantyDetail />
                  </AppShell>
                }
              />
              <Route
                path="/settings"
                element={
                  <AppShell>
                    <Settings />
                  </AppShell>
                }
              />
              <Route
                path="*"
                element={
                  <AppShell>
                    <Warranties />
                  </AppShell>
                }
              />
            </Routes>
          </Suspense>
        </LanguageRoot>
      </BrowserRouter>
      <Toaster
        position="bottom-right"
        richColors
        closeButton
        theme="system"
        toastOptions={{ className: "font-sans" }}
      />
    </QueryClientProvider>
  </StrictMode>,
);
