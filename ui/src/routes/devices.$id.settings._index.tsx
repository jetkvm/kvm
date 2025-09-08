import { redirect } from "react-router";
import type { LoaderFunction, LoaderFunctionArgs } from "react-router";

import { getDeviceUiPath } from "../hooks/useAppNavigation";

const loader: LoaderFunction = ({ params }: LoaderFunctionArgs) => {
  return redirect(getDeviceUiPath("/settings/general", params.id));
}

export default function SettingIndexRoute() {
  return (<></>);
}

SettingIndexRoute.loader = loader;