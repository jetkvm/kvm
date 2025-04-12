import { LoaderFunctionArgs, redirect } from "react-router-dom";

import { DEVICE_API } from "@/ui.config";

import api from "../api";

export interface CloudState {
  connected: boolean;
  url: string;
  appUrl: string;
}

const loader = async ({ request }: LoaderFunctionArgs) => {
  const url = new URL(request.url);
  const searchParams = url.searchParams;

  const tempToken = searchParams.get("tempToken");
  const deviceId = searchParams.get("deviceId");
  const oidcGoogle = searchParams.get("oidcGoogle");
  const clientId = searchParams.get("clientId");

  const [cloudStateResponse, registerResponse] = await Promise.all([
    api.GET(`${DEVICE_API}/cloud/state`),
    api.POST(`${DEVICE_API}/cloud/register`, {
      token: tempToken,
      oidcGoogle,
      clientId,
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
