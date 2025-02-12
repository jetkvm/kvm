import { useEffect, useState } from "react";
import GridBackground from "@components/GridBackground";
import Container from "@components/Container";
import { LinkButton } from "@components/Button";
import LogoBlueIcon from "@/assets/logo-blue.png";
import LogoWhiteIcon from "@/assets/logo-white.svg";
import DeviceImage from "@/assets/jetkvm-device-still.png";
import LogoMark from "@/assets/logo-mark.png";
import { cx } from "cva";
import api from "../api";
import { redirect } from "react-router-dom";
import { SIGNAL_API } from "@/ui.config";

export interface DeviceStatus {
  isSetup: boolean;
}

const loader = async () => {
  const res = await api
    .GET(`${SIGNAL_API}/device/status`)
    .then(res => res.json() as Promise<DeviceStatus>);

  if (res.isSetup) return redirect("/login-local");
  return null;
};

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
            <div className="flex items-center justify-center w-full h-full isolate">
              <div className="max-w-3xl text-center">
                <div className="space-y-8">
                  <div className="space-y-4">
                    <div className="flex items-center justify-center opacity-0 animate-fadeIn animation-delay-1000">
                      <img src={LogoWhiteIcon} alt="JetKVM Logo" className="h-[32px] hidden dark:block" />
                      <img src={LogoBlueIcon} alt="JetKVM Logo" className="h-[32px] dark:hidden" />
                    </div>

                    <div
                      className="space-y-1 opacity-0 animate-fadeIn"
                      style={{ animationDelay: "1500ms" }}
                    >
                      <h1 className="text-4xl font-semibold text-black dark:text-white">
                        Welcome to JetKVM
                      </h1>
                      <p className="text-lg font-medium text-slate-600 dark:text-slate-400">
                        Control any computer remotely
                      </p>
                    </div>
                  </div>

                  <div className="!-mt-2 -ml-6 flex items-center justify-center">
                    <img
                      src={DeviceImage}
                      alt="JetKVM Device"
                      className="animation-delay-0 max-w-md scale-[0.98] animate-fadeInScaleFloat opacity-0 transition-all duration-1000 ease-out"
                    />
                  </div>
                </div>
                <div className="-mt-8 space-y-4">
                  <p
                    style={{ animationDelay: "2000ms" }}
                    className="max-w-lg mx-auto text-lg opacity-0 animate-fadeIn text-slate-700 dark:text-slate-300"
                  >
                    JetKVM combines powerful hardware with intuitive software to provide a
                    seamless remote control experience.
                  </p>
                  <div
                    style={{ animationDelay: "2300ms" }}
                    className="opacity-0 animate-fadeIn"
                  >
                    <LinkButton
                      size="LG"
                      theme="light"
                      text="Set up your JetKVM"
                      LeadingIcon={({ className }) => (
                        <img src={LogoMark} className={cx(className, "mr-1.5 !h-5")} />
                      )}
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
