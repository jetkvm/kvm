import { useLocation, useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";

import AuthLayout from "@components/AuthLayout";

export default function SignupRoute() {
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
        action={t('Signup_Connect_device')}
        cta={t('Already_have_an_account')}
        ctaHref={`/login?${sq.toString()}`}
      />
    );
  }

  return (
    <AuthLayout
      title={t('Create_your_JetKVM_account')}
      description={t('Create_your_account_and_start_managing_your_devices_with_ease')}
      action={t('Create_Account')}
      // Header CTA
      cta={t('Already_have_an_account')}
      ctaHref={`/login?${sq.toString()}`}
    />
  );
}
