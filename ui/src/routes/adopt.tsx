import { redirect } from "react-router";
import type { LoaderFunction, LoaderFunctionArgs } from "react-router";

import { DEVICE_API } from "@/ui.config";
import api from "@/api";

export interface CloudState {
  connected: boolean;
  url: string;
  appUrl: string;
}

const loader: LoaderFunction = async ({ request }: LoaderFunctionArgs) => {
  const url = new URL(request.url);
  const searchParams = url.searchParams;

  const tempToken = searchParams.get("tempToken");
  const deviceId = searchParams.get("deviceId");
  const oidcToken = searchParams.get("oidcToken") ?? searchParams.get("oidcGoogle");
  const oidcClientId = searchParams.get("oidcClientId") ?? searchParams.get("clientId");
  const oidcIssuer = searchParams.get("oidcIssuer");

  const [cloudStateResponse, registerResponse] = await Promise.all([
    api.GET(`${DEVICE_API}/cloud/state`),
    api.POST(`${DEVICE_API}/cloud/register`, {
      token: tempToken,
      oidcToken,
      oidcClientId,
      oidcIssuer,
      oidcGoogle: oidcToken,
      clientId: oidcClientId,
    }),
  ]);

  if (!cloudStateResponse.ok) throw new Error("Failed to get cloud state");
  const cloudState = (await cloudStateResponse.json()) as CloudState;

  if (!registerResponse.ok) throw new Error("Failed to register device");

  return redirect(cloudState.appUrl + `/devices/${deviceId}/setup`);
};

export default function AdoptRoute() {
  return <></>;
}

AdoptRoute.loader = loader;
