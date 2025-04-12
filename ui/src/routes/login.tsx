import { useLocation, useSearchParams } from "react-router-dom";

import AuthLayout from "@components/AuthLayout";

export default function LoginRoute() {
  const [sq] = useSearchParams();
  const location = useLocation();
  const deviceId = sq.get("deviceId") || location.state?.deviceId;

  if (deviceId) {
    return (
      <AuthLayout
        showCounter={true}
        title="Connect your JetKVM to the cloud"
        description="Unlock remote access and advanced features for your device"
        action="Log in & Connect device"
        // Header CTA
        cta="Don't have an account?"
        ctaHref={`/signup?${sq.toString()}`}
      />
    );
  }

  return (
    <AuthLayout
      title="Log in to your JetKVM account"
      description="Log in to access and manage your devices securely"
      action="Log in"
      // Header CTA
      cta="New to JetKVM?"
      ctaHref={`/signup?${sq.toString()}`}
    />
  );
}
