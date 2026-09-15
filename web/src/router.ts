import { createBrowserRouter, redirect } from "react-router"

import { AppLayout } from "@/components/app-layout"
import { PageSpinner } from "@/components/page-spinner"
import { RequireAuth } from "@/components/require-auth"

export const router = createBrowserRouter([
  {
    HydrateFallback: PageSpinner,
    children: [
      {
        path: "/login",
        lazy: async () => ({ Component: (await import("@/pages/login")).LoginPage }),
      },
      {
        Component: RequireAuth,
        children: [
          {
            Component: AppLayout,
            children: [
              {
                index: true,
                lazy: async () => ({ Component: (await import("@/pages/overview")).OverviewPage }),
              },
              {
                path: "requests",
                lazy: async () => ({ Component: (await import("@/pages/requests")).RequestsPage }),
              },
              {
                path: "requests/:id",
                lazy: async () => ({
                  Component: (await import("@/pages/request-detail")).RequestDetailPage,
                }),
              },
              {
                path: "eval",
                lazy: async () => ({ Component: (await import("@/pages/eval")).EvalPage }),
              },
              {
                path: "eval/:id",
                lazy: async () => ({ Component: (await import("@/pages/eval-run")).EvalRunPage }),
              },
              {
                path: "providers",
                lazy: async () => ({ Component: (await import("@/pages/providers")).ProvidersPage }),
              },
              {
                path: "routes",
                lazy: async () => ({ Component: (await import("@/pages/routes")).RoutesPage }),
              },
              {
                path: "keys",
                lazy: async () => ({ Component: (await import("@/pages/keys")).KeysPage }),
              },
              {
                path: "settings",
                lazy: async () => ({ Component: (await import("@/pages/settings")).SettingsPage }),
              },
            ],
          },
        ],
      },
      { path: "*", loader: () => redirect("/") },
    ],
  },
])
