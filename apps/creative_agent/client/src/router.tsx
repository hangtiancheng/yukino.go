import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  redirect,
} from "@tanstack/react-router";

import { ConversationPage } from "@/routes/conversation";
import { HomePage } from "@/routes/home";
import { LoginPage } from "@/routes/login";
import { RegisterPage } from "@/routes/register";
import { useAuthStore } from "@/stores/auth";

const rootRoute = createRootRoute({
  component: () => <Outlet />,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  component: LoginPage,
});

const registerRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/register",
  component: RegisterPage,
});

/** 登录守卫：无 token 跳登录页。 */
function requireAuth() {
  if (!useAuthStore.getState().token) {
    throw redirect({ to: "/login" });
  }
}

const homeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: requireAuth,
  component: HomePage,
});

const conversationRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/conversation",
  beforeLoad: requireAuth,
  component: ConversationPage,
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  registerRoute,
  homeRoute,
  conversationRoute,
]);

export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
