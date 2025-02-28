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
import DeviceRoute, { LocalDevice } from "@routes/devices.$id";
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
import WelcomeRoute, { DeviceStatus } from "./routes/welcome-local";
import WelcomeLocalPasswordRoute from "./routes/welcome-local.password";
import { CLOUD_API, DEVICE_API } from "./ui.config";
import OtherSessionRoute from "./routes/devices.$id.other-session";
import MountRoute from "./routes/devices.$id.mount";
import * as SettingsRoute from "./routes/devices.$id.settings";
import SettingsKeyboardMouseRoute from "./routes/devices.$id.settings.mouse";
import api from "./api";
import * as SettingsIndexRoute from "./routes/devices.$id.settings._index";
import SettingsAdvancedRoute from "./routes/devices.$id.settings.advanced";
import * as SettingsAccessIndexRoute from "./routes/devices.$id.settings.access._index";
import SettingsHardwareRoute from "./routes/devices.$id.settings.hardware";
import SettingsVideoRoute from "./routes/devices.$id.settings.video";
import SettingsAppearanceRoute from "./routes/devices.$id.settings.appearance";
import * as SettingsGeneralIndexRoute from "./routes/devices.$id.settings.general._index";
import SettingsGeneralUpdateRoute from "./routes/devices.$id.settings.general.update";
import SecurityAccessLocalAuthRoute from "./routes/devices.$id.settings.access.local-auth";

export const isOnDevice = import.meta.env.MODE === "device";
export const isInCloud = !isOnDevice;

export async function checkCloudAuth() {
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

export async function checkDeviceAuth() {
  const res = await api
    .GET(`${DEVICE_API}/device/status`)
    .then(res => res.json() as Promise<DeviceStatus>);

  if (!res.isSetup) return redirect("/welcome");

  const deviceRes = await api.GET(`${DEVICE_API}/device`);
  if (deviceRes.status === 401) return redirect("/login-local");
  if (deviceRes.ok) {
    const device = (await deviceRes.json()) as LocalDevice;
    return { authMode: device.authMode };
  }

  throw new Error("Error fetching device");
}

export async function checkAuth() {
  return import.meta.env.MODE === "device" ? checkDeviceAuth() : checkCloudAuth();
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
      children: [
        {
          path: "other-session",
          element: <OtherSessionRoute />,
        },
        {
          path: "mount",
          element: <MountRoute />,
        },
        {
          path: "settings",
          element: <SettingsRoute.default />,
          children: [
            {
              index: true,
              loader: SettingsIndexRoute.loader,
            },
            {
              path: "general",
              children: [
                {
                  index: true,
                  element: <SettingsGeneralIndexRoute.default />,
                },
                {
                  path: "update",
                  element: <SettingsGeneralUpdateRoute />,
                },
              ],
            },
            {
              path: "mouse",
              element: <SettingsKeyboardMouseRoute />,
            },
            {
              path: "advanced",
              element: <SettingsAdvancedRoute />,
            },
            {
              path: "hardware",
              element: <SettingsHardwareRoute />,
            },
            {
              path: "access",
              children: [
                {
                  index: true,
                  element: <SettingsAccessIndexRoute.default />,
                  loader: SettingsAccessIndexRoute.loader,
                },
                {
                  path: "local-auth",
                  element: <SecurityAccessLocalAuthRoute />,
                },
              ],
            },
            {
              path: "video",
              element: <SettingsVideoRoute />,
            },
            {
              path: "appearance",
              element: <SettingsAppearanceRoute />,
            },
          ],
        },
      ],
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
              children: [
                {
                  path: "other-session",
                  element: <OtherSessionRoute />,
                },
                {
                  path: "mount",
                  element: <MountRoute />,
                },
                {
                  path: "settings",
                  element: <SettingsRoute.default />,
                  children: [
                    {
                      index: true,
                      loader: SettingsIndexRoute.loader,
                    },
                    {
                      path: "general",
                      children: [
                        {
                          index: true,
                          element: <SettingsGeneralIndexRoute.default />,
                        },
                        {
                          path: "update",
                          element: <SettingsGeneralUpdateRoute />,
                        },
                      ],
                    },
                    {
                      path: "mouse",
                      element: <SettingsKeyboardMouseRoute />,
                    },
                    {
                      path: "advanced",
                      element: <SettingsAdvancedRoute />,
                    },
                    {
                      path: "hardware",
                      element: <SettingsHardwareRoute />,
                    },
                    {
                      path: "access",
                      children: [
                        {
                          index: true,
                          element: <SettingsAccessIndexRoute.default />,
                          loader: SettingsAccessIndexRoute.loader,
                        },
                        {
                          path: "local-auth",
                          element: <SecurityAccessLocalAuthRoute />,
                        },
                      ],
                    },
                    {
                      path: "video",
                      element: <SettingsVideoRoute />,
                    },
                    {
                      path: "appearance",
                      element: <SettingsAppearanceRoute />,
                    },
                  ],
                },
              ],
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
    <>
      <RouterProvider router={router} />
      <Notifications
        toastOptions={{
          className:
            "rounded border-none bg-white text-black shadow outline outline-1 outline-slate-800/30",
        }}
        max={2}
      />
    </>,
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
    <div className="h-full w-full">
      <div className="flex h-full items-center justify-center">
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
