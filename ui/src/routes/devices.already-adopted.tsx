import { useTranslation } from "react-i18next";
import { LinkButton } from "@/components/Button";
import SimpleNavbar from "@/components/SimpleNavbar";
import Container from "@/components/Container";
import GridBackground from "@components/GridBackground";

export default function DevicesAlreadyAdopted() {
  const { t } = useTranslation();
  return (
    <>
      <GridBackground />

      <div className="grid min-h-screen grid-rows-(--grid-layout)">
        <SimpleNavbar />
        <Container>
          <div className="flex items-center justify-center w-full h-full isolate">
            <div className="max-w-2xl -mt-16 space-y-8">
              <div className="space-y-4 text-center">
                <h1 className="text-4xl font-semibold text-black dark:text-white">{t('Device_Already_Registered')}</h1>
                <p className="text-lg text-slate-600 dark:text-slate-400">
                    {t('This_device_is_currently_registered_to_another_user_in_our_cloud_dashboard')}
                </p>
                <p className="mt-4 text-lg text-slate-600 dark:text-slate-400">
                    {t('already_registered_notice')}
                </p>
              </div>

              <div className="text-center">
                <LinkButton
                  to="/devices"
                  size="LG"
                  theme="primary"
                  text={t('Return_to_Dashboard')}
                />
              </div>
            </div>
          </div>
        </Container>
      </div>
    </>
  );
}
