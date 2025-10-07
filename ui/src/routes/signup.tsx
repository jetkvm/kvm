import { useLocation, useSearchParams } from "react-router";

import AuthLayout from "@components/AuthLayout";
import { m } from "@localizations/messages.js";

export default function SignupRoute() {
  const [sq] = useSearchParams();
  const location = useLocation();
  const deviceId = sq.get("deviceId") || location.state?.deviceId;

  if (deviceId) {
    return (
      <AuthLayout
        showCounter={true}
        title={m.auth_connect_to_cloud()}
        description={m.auth_connect_to_cloud_description()}
        action={m.auth_signup_connect_to_cloud_action()}
        cta={m.auth_header_cta_already_have_account()}
        ctaHref={`/login?${sq.toString()}`}
      />
    );
  }

  return (
    <AuthLayout
      title={m.auth_signup_create_account()}
      description={m.auth_signup_create_account_description()}
      action={m.auth_signup_create_account_action()}
      // Header CTA
      cta={m.auth_header_cta_already_have_account()}
      ctaHref={`/login?${sq.toString()}`}
    />
  );
}
