import { LoaderFunctionArgs, redirect } from "react-router-dom";
import api from "../api";
import { CLOUD_API, CLOUD_APP, SIGNAL_API } from "@/ui.config";

const loader = async ({ request }: LoaderFunctionArgs) => {
  const url = new URL(request.url);
  const searchParams = url.searchParams;

  const tempToken = searchParams.get("tempToken");
  const deviceId = searchParams.get("deviceId");
  const oidcGoogle = searchParams.get("oidcGoogle");
  const clientId = searchParams.get("clientId");

  const res = await api.POST(
    `${SIGNAL_API}/cloud/register`,
    {
      token: tempToken,
      cloudApi: CLOUD_API,
      oidcGoogle,
      clientId,
    },
  );

  if (!res.ok) throw new Error("Failed to register device");
  return redirect(CLOUD_APP + `/devices/${deviceId}/setup`);
};

export default function AdoptRoute() {
  return <></>;
}

AdoptRoute.loader = loader;
