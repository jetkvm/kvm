import { Button, LinkButton } from "@components/Button";
import { GoogleIcon } from "@components/Icons";
import SimpleNavbar from "@components/SimpleNavbar";
import Container from "@components/Container";
import { useLocation, useNavigation, useSearchParams } from "react-router-dom";
import Fieldset from "@components/Fieldset";
import GridBackground from "@components/GridBackground";
import StepCounter from "@components/StepCounter";
import { CLOUD_API } from "@/ui.config";

type AuthLayoutProps = {
  title: string;
  description: string;
  action: string;
  cta: string;
  ctaHref: string;
  showCounter?: boolean;
};

export default function AuthLayout({
  title,
  description,
  action,
  cta,
  ctaHref,
  showCounter,
}: AuthLayoutProps) {
  const [sq] = useSearchParams();
  const location = useLocation();

  const returnTo = sq.get("returnTo") || location.state?.returnTo;
  const deviceId = sq.get("deviceId") || location.state?.deviceId;
  const navigation = useNavigation();

  return (
    <>
      <GridBackground />

      <div className="grid min-h-screen grid-rows-layout">
        <SimpleNavbar
          logoHref="/"
          actionElement={
            <div>
              <LinkButton to={ctaHref} text={cta} theme="light" size="MD" />
            </div>
          }
        />
        <Container>
          <div className="flex items-center justify-center w-full h-full isolate">
            <div className="max-w-2xl -mt-16 space-y-8">
              {showCounter ? (
                <div className="text-center">
                  <StepCounter currStepIdx={0} nSteps={2} />
                </div>
              ) : null}
              <div className="space-y-2 text-center">
                <h1 className="text-4xl font-semibold text-black dark:text-white">
                  {title}
                </h1>
                <p className="text-slate-600 dark:text-slate-400">{description}</p>
              </div>

              <Fieldset className="space-y-12">
                <div className="max-w-sm mx-auto space-y-4">
                  <form
                    action={`${CLOUD_API}/oidc/google`}
                    method="POST"
                  >
                    {/*This could be the KVM ID*/}
                    {deviceId ? (
                      <input type="hidden" name="deviceId" value={deviceId} />
                    ) : null}
                    {returnTo ? (
                      <input type="hidden" name="returnTo" value={returnTo} />
                    ) : null}
                    <Button
                      size="LG"
                      theme="light"
                      fullWidth
                      text={`${action}`}
                      LeadingIcon={GoogleIcon}
                      textAlign="center"
                      type="submit"
                      loading={
                        (navigation.state === "submitting" ||
                          navigation.state === "loading") &&
                        navigation.formMethod?.toLowerCase() === "post" &&
                        navigation.formAction?.includes("auth/google")
                      }
                    />
                  </form>
                </div>
              </Fieldset>
            </div>
          </div>
        </Container>
      </div>
    </>
  );
}
