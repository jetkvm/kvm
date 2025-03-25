import { LoaderFunctionArgs, redirect } from "react-router-dom";

import { getDeviceUiPath } from "../hooks/useAppNavigation";

export function loader({ params }: LoaderFunctionArgs) {
  return redirect(getDeviceUiPath("/settings/general", params.id));
}
