import { LoaderFunctionArgs, redirect } from "react-router-dom";
import api from "../api";

const loader = async ({ request }: LoaderFunctionArgs) => {
  const url = new URL(request.url);
  const searchParams = url.searchParams;

  const tempToken = searchParams.get("tempToken");
  const deviceId = searchParams.get("deviceId");
  const oidcGoogle = searchParams.get("oidcGoogle");
  const clientId = searchParams.get("clientId");

  const res = await api.POST(
    `${import.meta.env.VITE_SIGNAL_API}/cloud/register`,
    {
      token: tempToken,
      cloudApi: import.meta.env.VITE_CLOUD_API,
      oidcGoogle,
      clientId,
    },
  );

  if (!res.ok) throw new Error("Failed to register device");
  return redirect(import.meta.env.VITE_CLOUD_APP + `/devices/${deviceId}/setup`);
};

export default function AdoptRoute() {
  return <></>;
}

AdoptRoute.loader = loader;
