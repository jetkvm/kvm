import { useEffect, useState } from "react";
import { redirect } from "react-router";
import type { LoaderFunction } from "react-router";
import { cx } from "cva";

import LogoBlueIcon from "@assets/logo-blue.png";
import LogoWhiteIcon from "@assets/logo-white.svg";
import DeviceImage from "@assets/jetkvm-device-still.png";
import LogoMark from "@assets/logo-mark.png";
import Container from "@components/Container";
import GridBackground from "@components/GridBackground";
import { LinkButton } from "@components/Button";
import api from "@/api";
import { DEVICE_API } from "@/ui.config";
import { m } from "@localizations/messages.js";

export interface DeviceStatus {
  isSetup: boolean;
}

const loader: LoaderFunction = async () => {
  const res = await api
    .GET(`${DEVICE_API}/device/status`)
    .then(res => res.json() as Promise<DeviceStatus>);

  if (res.isSetup) return redirect("/login-local");
  return null;
};

const LogoLeadingIcon = ({ className }: { className?: string }) => (
  <img src={LogoMark} className={cx(className, "mr-1.5 h-5!")} alt={m.jetkvm_logo()} />
);

export default function WelcomeRoute() {
  const [imageLoaded, setImageLoaded] = useState(false);

  useEffect(() => {
    const img = new Image();
    img.src = DeviceImage;
    img.onload = () => setImageLoaded(true);
  }, []);

  return (
    <>
      <GridBackground />
      <div className="grid min-h-screen">
        {imageLoaded && (
          <Container>
            <div className="isolate flex h-full w-full items-center justify-center">
              <div className="max-w-3xl text-center">
                <div className="space-y-8">
                  <div className="space-y-4">
                    <div className="flex animate-fadeIn items-center justify-center opacity-0 animation-delay-1000">
                      <img
                        src={LogoWhiteIcon}
                        alt={m.jetkvm_logo()}
                        className="hidden h-8 dark:block"
                      />
                      <img src={LogoBlueIcon} alt={m.jetkvm_logo()} className="h-8 dark:hidden" />
                    </div>

                    <div className="animate-fadeIn space-y-1 opacity-0 animation-delay-1500">
                      <h1 className="text-4xl font-semibold text-black dark:text-white">
                        {m.welcome_to_jetkvm()}
                      </h1>
                      <p className="text-lg font-medium text-slate-600 dark:text-slate-400">
                        {m.welcome_to_jetkvm_description()}
                      </p>
                    </div>
                  </div>

                  <div className="-mt-2! -ml-6 flex items-center justify-center">
                    <img
                      src={DeviceImage}
                      alt={m.jetkvm_device()}
                      className="max-w-md scale-[0.98] animate-fadeInScaleFloat opacity-0 transition-all duration-1000 ease-out animation-delay-300"
                    />
                  </div>
                </div>
                <div className="-mt-8 space-y-4">
                  <p
                    style={{ animationDelay: "2000ms" }}
                    className="mx-auto max-w-lg animate-fadeIn text-lg text-slate-700 opacity-0 dark:text-slate-300"
                  >
                    {m.jetkvm_description()}
                  </p>
                  <div className="animate-fadeIn opacity-0 animation-delay-2300">
                    <LinkButton
                      size="LG"
                      theme="light"
                      text={m.jetkvm_setup()}
                      LeadingIcon={LogoLeadingIcon}
                      textAlign="center"
                      to="/welcome/mode"
                    />
                  </div>
                </div>
              </div>
            </div>
          </Container>
        )}
      </div>
    </>
  );
}

WelcomeRoute.loader = loader;
