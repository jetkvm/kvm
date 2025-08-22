import { LoaderFunctionArgs, redirect } from "react-router-dom";

import { getDeviceUiPath } from "../hooks/useAppNavigation";

const loader = async ({ params }: LoaderFunctionArgs) => {
  return redirect(getDeviceUiPath("/settings/general", params.id));
}

export default function SettingIndexRoute() {
  return (<></>);
}

SettingIndexRoute.loader = loader;