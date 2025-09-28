import { useTranslation } from "react-i18next";
import { useLocation, useSearchParams } from "react-router";

import AuthLayout from "@components/AuthLayout";

export default function LoginRoute() {
  const { t } = useTranslation();
  const [sq] = useSearchParams();
  const location = useLocation();
  const deviceId = sq.get("deviceId") || location.state?.deviceId;

  if (deviceId) {
    return (
      <AuthLayout
        showCounter={true}
        title={t('Connect_your_JetKVM_to_the_cloud')}
        description={t('Unlock_remote_access_and_advanced_features_for_your_device')}
        action={t('Log_in_Connect_device')}
        // Header CTA
        cta={t('Dont_have_an_account')}
        ctaHref={`/signup?${sq.toString()}`}
      />
    );
  }

  return (
    <AuthLayout
      title={t('Log_in_to_your_JetKVM_account')}
      description={t('Log_in_to_access_and_manage_your_devices_securely')}
      action={t('Log_In')}
      // Header CTA
      cta={t('New_to_JetKVM')}
      ctaHref={`/signup?${sq.toString()}`}
    />
  );
}
