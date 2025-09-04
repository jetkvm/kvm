import { useEffect, useState } from "react";
import { cx } from "cva";
import { redirect } from "react-router";
import type { LoaderFunction } from "react-router";

import GridBackground from "@components/GridBackground";
import Container from "@components/Container";
import { LinkButton } from "@components/Button";
import LogoBlueIcon from "@/assets/logo-blue.png";
import LogoWhiteIcon from "@/assets/logo-white.svg";
import DeviceImage from "@/assets/jetkvm-device-still.png";
import LogoMark from "@/assets/logo-mark.png";
import { DEVICE_API } from "@/ui.config";

import api from "../api";

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
                    <div className="animate-fadeIn animation-delay-1000 flex items-center justify-center opacity-0">
                      <img
                        src={LogoWhiteIcon}
                        alt="JetKVM Logo"
                        className="hidden h-[32px] dark:block"
                      />
                      <img
                        src={LogoBlueIcon}
                        alt="JetKVM Logo"
                        className="h-[32px] dark:hidden"
                      />
                    </div>

                    <div className="animate-fadeIn animation-delay-1500 space-y-1 opacity-0">
                      <h1 className="text-4xl font-semibold text-black dark:text-white">
                        Welcome to JetKVM
                      </h1>
                      <p className="text-lg font-medium text-slate-600 dark:text-slate-400">
                        Control any computer remotely
                      </p>
                    </div>
                  </div>

                  <div className="-mt-2! -ml-6 flex items-center justify-center">
                    <img
                      src={DeviceImage}
                      alt="JetKVM Device"
                      className="animation-delay-300 animate-fadeInScaleFloat max-w-md scale-[0.98] opacity-0 transition-all duration-1000 ease-out"
                    />
                  </div>
                </div>
                <div className="-mt-8 space-y-4">
                  <p
                    style={{ animationDelay: "2000ms" }}
                    className="animate-fadeIn mx-auto max-w-lg text-lg text-slate-700 opacity-0 dark:text-slate-300"
                  >
                    JetKVM combines powerful hardware with intuitive software to provide a
                    seamless remote control experience.
                  </p>
                  <div className="animate-fadeIn animation-delay-2300 opacity-0">
                    <LinkButton
                      size="LG"
                      theme="light"
                      text="Set up your JetKVM"
                      LeadingIcon={({ className }) => (
                        <img src={LogoMark} className={cx(className, "mr-1.5 h-5!")} />
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
