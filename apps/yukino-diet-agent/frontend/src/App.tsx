import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HashRouter, Navigate, Route, Routes } from "react-router-dom";

import { AppShell } from "@/components/layout/app-shell";
import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import ChatPage from "@/pages/chat";
import DashboardPage from "@/pages/dashboard";
import EvaluationsPage from "@/pages/evaluations";
import PersonalMealsPage from "@/pages/personal-meals";
import PublicMealsPage from "@/pages/public-meals";
import TracesPage from "@/pages/traces";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 30 * 1000,
    },
  },
});

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <HashRouter>
          <Routes>
            <Route element={<AppShell />}>
              <Route index element={<DashboardPage />} />
              <Route path="/chat" element={<ChatPage />} />
              <Route path="/meals/personal" element={<PersonalMealsPage />} />
              <Route path="/meals/public" element={<PublicMealsPage />} />
              <Route path="/traces" element={<TracesPage />} />
              <Route path="/evaluations" element={<EvaluationsPage />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Route>
          </Routes>
        </HashRouter>
        <Toaster position="top-right" richColors closeButton />
      </TooltipProvider>
    </QueryClientProvider>
  );
}
