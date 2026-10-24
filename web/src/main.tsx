import { QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Route, Routes } from "react-router";
import { Toaster } from "sonner";
import { AppShell } from "./components/AppShell";
import "./index.css";
import { queryClient } from "./lib/queries";
import { Login } from "./pages/Login";
import { Settings } from "./pages/Settings";
import { WarrantyDetail } from "./pages/WarrantyDetail";
import { Warranties } from "./pages/Warranties";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/" element={<AppShell><Warranties /></AppShell>} />
          <Route path="/w/:id" element={<AppShell><WarrantyDetail /></AppShell>} />
          <Route path="/settings" element={<AppShell><Settings /></AppShell>} />
          <Route path="*" element={<AppShell><Warranties /></AppShell>} />
        </Routes>
      </BrowserRouter>
      <Toaster position="bottom-right" richColors closeButton theme="system" toastOptions={{ className: "font-sans" }} />
    </QueryClientProvider>
  </StrictMode>,
);
