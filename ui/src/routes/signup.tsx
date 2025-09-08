import { useLocation, useSearchParams } from "react-router";

import AuthLayout from "@components/AuthLayout";

export default function SignupRoute() {
  const [sq] = useSearchParams();
  const location = useLocation();
  const deviceId = sq.get("deviceId") || location.state?.deviceId;

  if (deviceId) {
    return (
      <AuthLayout
        showCounter={true}
        title="Connect your JetKVM to the cloud"
        description="Unlock remote access and advanced features for your device."
        action="Signup & Connect device"
        cta="Already have an account?"
        ctaHref={`/login?${sq.toString()}`}
      />
    );
  }

  return (
    <AuthLayout
      title="Create your JetKVM account"
      description="Create your account and start managing your devices with ease."
      action="Create Account"
      // Header CTA
      cta="Already have an account?"
      ctaHref={`/login?${sq.toString()}`}
    />
  );
}
