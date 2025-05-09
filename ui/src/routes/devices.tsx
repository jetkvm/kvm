import { useLoaderData, useRevalidator } from "react-router-dom";
import { LuMonitorSmartphone } from "react-icons/lu";
import { ArrowRightIcon } from "@heroicons/react/16/solid";

import DashboardNavbar from "@components/Header";
import { LinkButton } from "@components/Button";
import KvmCard from "@components/KvmCard";
import { useInterval } from "usehooks-ts";
import { checkAuth } from "@/main";
import { User } from "@/hooks/stores";
import EmptyCard from "@components/EmptyCard";
import { CLOUD_API } from "@/ui.config";

interface LoaderData {
  devices: { id: string; name: string; online: boolean; lastSeen: string }[];
  user: User;
}

export const loader = async () => {
  const user = await checkAuth();

  try {
    const res = await fetch(`${CLOUD_API}/devices`, {
      method: "GET",
      credentials: "include",
      mode: "cors",
    });

    const { devices } = await res.json();
    return { devices, user };
  } catch (e) {
    console.error(e);
    return { devices: [] };
  }
};

export default function DevicesRoute() {
  const { devices, user } = useLoaderData() as LoaderData;
  const revalidate = useRevalidator();
  useInterval(revalidate.revalidate, 4000);
  return (
    <div className="relative h-full">
      <div className="grid h-full select-none grid-rows-headerBody">
        <DashboardNavbar
          isLoggedIn={!!user}
          primaryLinks={[{ title: "Cloud Devices", to: "/devices" }]}
          userEmail={user?.email}
          picture={user?.picture}
        />

        <div className="flex h-full overflow-hidden">
          <div className="mx-auto h-full w-full space-y-6 px-4 sm:max-w-6xl sm:px-8 md:max-w-7xl md:px-12 lg:max-w-8xl">
            <div className="mt-8 flex items-center justify-between border-b border-b-slate-800/20 pb-4 dark:border-b-slate-300/20">
              <div>
                <h1 className="text-xl font-bold text-black dark:text-white">
                  Cloud KVMs
                </h1>
                <p className="text-base text-slate-700 dark:text-slate-400">
                  Manage your cloud KVMs and connect to them securely.
                </p>
              </div>
            </div>

            {devices.length === 0 ? (
              <div className="max-w-3xl">
                <EmptyCard
                  IconElm={LuMonitorSmartphone}
                  headline="No devices found"
                  description="You don't have any devices with enabled JetKVM Cloud yet."
                  BtnElm={
                    <LinkButton
                      to="https://jetkvm.com/docs/networking/remote-access"
                      size="SM"
                      theme="primary"
                      TrailingIcon={ArrowRightIcon}
                      text="Learn more"
                    />
                  }
                />
              </div>
            ) : (
              <>
                <div className="grid gap-4 sm:grid-cols-2 md:grid-cols-3">
                  {devices.map(x => {
                    return (
                      <KvmCard
                        key={x.id}
                        id={x.id}
                        title={x.name ?? x.id}
                        lastSeen={x.lastSeen ? new Date(x.lastSeen) : null}
                        online={x.online}
                      />
                    );
                  })}
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
