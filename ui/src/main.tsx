import React from "react";
import ReactDOM from "react-dom/client";
import Root from "./root";
import "./index.css";
import {
  createBrowserRouter,
  isRouteErrorResponse,
  redirect,
  RouterProvider,
  useRouteError,
} from "react-router-dom";
import DeviceRoute from "@routes/devices.$id";
import DevicesRoute, { loader as DeviceListLoader } from "@routes/devices";
import SetupRoute from "@routes/devices.$id.setup";
import LoginRoute from "@routes/login";
import SignupRoute from "@routes/signup";
import AdoptRoute from "@routes/adopt";
import DeviceIdRename from "@routes/devices.$id.rename";
import DevicesIdDeregister from "@routes/devices.$id.deregister";
import NotFoundPage from "@components/NotFoundPage";
import EmptyCard from "@components/EmptyCard";
import { ExclamationTriangleIcon } from "@heroicons/react/16/solid";
import Card from "@components/Card";
import DevicesAlreadyAdopted from "@routes/devices.already-adopted";
import Notifications from "./notifications";
import LoginLocalRoute from "./routes/login-local";
import WelcomeLocalModeRoute from "./routes/welcome-local.mode";
import WelcomeRoute from "./routes/welcome-local";
import WelcomeLocalPasswordRoute from "./routes/welcome-local.password";
import { CLOUD_API } from "./ui.config";

export const isOnDevice = import.meta.env.MODE === "device";
export const isInCloud = !isOnDevice;

export async function checkAuth() {
  const res = await fetch(`${CLOUD_API}/me`, {
    mode: "cors",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
  });

  if (res.status === 401) {
    throw redirect(`/login?returnTo=${window.location.href}`);
  }

  return await res.json();
}

let router;
if (isOnDevice) {
  router = createBrowserRouter([
    {
      path: "/welcome/mode",
      element: <WelcomeLocalModeRoute />,
      action: WelcomeLocalModeRoute.action,
    },
    {
      path: "/welcome/password",
      element: <WelcomeLocalPasswordRoute />,
      action: WelcomeLocalPasswordRoute.action,
    },
    {
      path: "/welcome",
      element: <WelcomeRoute />,
      loader: WelcomeRoute.loader,
    },
    {
      path: "/login-local",
      element: <LoginLocalRoute />,
      action: LoginLocalRoute.action,
      loader: LoginLocalRoute.loader,
    },
    {
      path: "/",
      errorElement: <ErrorBoundary />,
      element: <DeviceRoute />,
      loader: DeviceRoute.loader,
    },
    {
      path: "/adopt",
      element: <AdoptRoute />,
      loader: AdoptRoute.loader,
      errorElement: <ErrorBoundary />,
    },
  ]);
} else {
  router = createBrowserRouter([
    {
      errorElement: <ErrorBoundary />,
      children: [
        { path: "signup", element: <SignupRoute /> },
        { path: "login", element: <LoginRoute /> },
        {
          path: "/",
          element: <Root />,
          children: [
            {
              index: true,
              loader: async () => {
                await checkAuth();
                return redirect(`/devices`);
              },
            },

            {
              path: "devices/:id/setup",
              element: <SetupRoute />,
              action: SetupRoute.action,
              loader: SetupRoute.loader,
            },
            {
              path: "devices/already-adopted",
              element: <DevicesAlreadyAdopted />,
            },
            {
              path: "devices/:id",
              element: <DeviceRoute />,
              loader: DeviceRoute.loader,
            },
            {
              path: "devices/:id/deregister",
              element: <DevicesIdDeregister />,
              loader: DevicesIdDeregister.loader,
              action: DevicesIdDeregister.action,
            },
            {
              path: "devices/:id/rename",
              element: <DeviceIdRename />,
              loader: DeviceIdRename.loader,
              action: DeviceIdRename.action,
            },
            { path: "devices", element: <DevicesRoute />, loader: DeviceListLoader },
          ],
        },
      ],
    },
  ]);
}

document.addEventListener("DOMContentLoaded", () => {
  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
      <RouterProvider router={router} />
      <Notifications
        toastOptions={{
          className:
            "rounded border-none bg-white text-black shadow outline outline-1 outline-slate-800/30",
        }}
        max={2}
      />
    </React.StrictMode>,
  );
});

// eslint-disable-next-line react-refresh/only-export-components
function ErrorBoundary() {
  const error = useRouteError();

  // eslint-disable-next-line @typescript-eslint/ban-ts-comment
  // @ts-expect-error
  const errorMessage = error?.data?.error?.message || error?.message;
  if (isRouteErrorResponse(error)) {
    if (error.status === 404) return <NotFoundPage />;
  }

  return (
    <div className="w-full h-full">
      <div className="flex items-center justify-center h-full">
        <div className="w-full max-w-2xl">
          <EmptyCard
            IconElm={ExclamationTriangleIcon}
            headline="Oh no!"
            description="Something went wrong. Please try again later or contact support"
            BtnElm={
              errorMessage && (
                <Card>
                  <div className="flex items-center font-mono">
                    <div className="flex p-2 text-black dark:text-white">
                      <span className="text-sm">{errorMessage}</span>
                    </div>
                  </div>
                </Card>
              )
            }
          />
        </div>
      </div>
    </div>
  );
}
