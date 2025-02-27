import { SectionHeader } from "@/components/SectionHeader";
import { SettingsItem } from "./devices.$id.settings";
import { useLoaderData } from "react-router-dom";
import { Button } from "../components/Button";
import { DEVICE_API } from "../ui.config";
import api from "../api";
import { LocalDevice } from "./devices.$id";
import { useDeviceUiNavigation } from "../hooks/useAppNavigation";

export const loader = async () => {
  const status = await api
    .GET(`${DEVICE_API}/device`)
    .then(res => res.json() as Promise<LocalDevice>);
  return status;
};

export default function SettingsSecurityIndexRoute() {
  const { authMode } = useLoaderData() as LocalDevice;
  const { navigateTo } = useDeviceUiNavigation();

  return (
    <div className="space-y-4">
      <SectionHeader
        title="Local Access"
        description="Manage the mode of local access to the device"
      />

      <div className="space-y-4">
        <SettingsItem
          title="Authentication Mode"
          description={`Current mode: ${authMode === "password" ? "Password protected" : "No password"}`}
        >
          {authMode === "password" ? (
            <Button
              size="SM"
              theme="light"
              text="Disable Protection"
              onClick={() => {
                navigateTo("./local-auth", { state: { init: "deletePassword" } });
              }}
            />
          ) : (
            <Button
              size="SM"
              theme="light"
              text="Enable Password"
              onClick={() => {
                navigateTo("./local-auth", { state: { init: "createPassword" } });
              }}
            />
          )}
        </SettingsItem>

        {authMode === "password" && (
          <SettingsItem
            title="Change Password"
            description="Update your device access password"
          >
            <Button
              size="SM"
              theme="light"
              text="Change Password"
              onClick={() => {
                navigateTo("./local-auth", { state: { init: "updatePassword" } });
              }}
            />
          </SettingsItem>
        )}
      </div>
    </div>
  );
}
