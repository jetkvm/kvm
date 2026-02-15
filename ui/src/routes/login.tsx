import { useEffect } from "react";
import { useLocation, useSearchParams } from "react-router";

import { m } from "@localizations/messages.js";
import AuthLayout from "@components/AuthLayout";
import { useSettingsStore } from "@/hooks/stores";

export default function LoginRoute() {
  const [sq] = useSearchParams();
  const location = useLocation();
  const deviceId = sq.get("deviceId") || location.state?.deviceId;

  useEffect(() => {
    useSettingsStore.getState().resetMicrophoneState();
  }, []);

  if (deviceId) {
    return (
      <AuthLayout
        showCounter={true}
        title={m.auth_connect_to_cloud()}
        description={m.auth_connect_to_cloud_description()}
        action={m.auth_connect_to_cloud_action()}
        // Header CTA
        cta={m.auth_header_cta_dont_have_account()}
        ctaHref={`/signup?${sq.toString()}`}
      />
    );
  }

  return (
    <AuthLayout
      title={m.auth_login()}
      description={m.auth_login_description()}
      action={m.auth_login_action()}
      // Header CTA
      cta={m.auth_header_cta_new_to_jetkvm()}
      ctaHref={`/signup?${sq.toString()}`}
    />
  );
}
