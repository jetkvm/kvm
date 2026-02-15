import { PlusCircleIcon } from "@heroicons/react/20/solid";
import { forwardRef, useEffect, useCallback } from "react";
import { LuLink, LuPlus, LuRadioReceiver } from "react-icons/lu";
import { useClose } from "@headlessui/react";
import { useLocation } from "react-router";

import { m } from "@localizations/messages.js";
import { Button } from "@components/Button";
import Card, { GridCard } from "@components/Card";
import { formatters } from "@/utils";
import { RemoteVirtualMediaState, useMountMediaStore } from "@hooks/stores";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import notifications from "@/notifications";

const MountPopopover = forwardRef<HTMLDivElement, object>((_props, ref) => {
  const { send } = useJsonRpc();
  const { remoteVirtualMediaState, setModalView, setRemoteVirtualMediaState } =
    useMountMediaStore();

  const syncRemoteVirtualMediaState = useCallback(() => {
    send("getVirtualMediaState", {}, (response: JsonRpcResponse) => {
      if ("error" in response) {
        notifications.error(m.mount_get_state_error({ error: response.error.message }));
      } else {
        setRemoteVirtualMediaState(response.result as unknown as RemoteVirtualMediaState);
      }
    });
  }, [send, setRemoteVirtualMediaState]);

  const handleUnmount = () => {
    send("unmountImage", {}, (response: JsonRpcResponse) => {
      if ("error" in response) {
        notifications.error(m.mount_unmount_error({ error: response.error.message }));
      } else {
        syncRemoteVirtualMediaState();
      }
    });
  };

  const renderGridCardContent = () => {
    if (!remoteVirtualMediaState) {
      return (
        <div className="space-y-1">
          <div className="inline-block">
            <Card>
              <div className="p-1">
                <PlusCircleIcon className="h-4 w-4 shrink-0 text-blue-700 dark:text-white" />
              </div>
            </Card>
          </div>
          <div className="space-y-1">
            <h3 className="text-sm leading-none font-semibold text-black dark:text-white">
              {m.mount_no_mounted_media()}
            </h3>
            <p className="text-xs leading-none text-slate-700 dark:text-slate-300">
              {m.mount_add_file_to_get_started()}
            </p>
          </div>
        </div>
      );
    }

    const { source, filename, size, url, path } = remoteVirtualMediaState;

    switch (source) {
      case "HTTP":
        return (
          <div className="">
            <div className="mb-0 inline-block">
              <Card>
                <div className="p-1">
                  <LuLink className="h-4 w-4 shrink-0 text-blue-700 dark:text-blue-500" />
                </div>
              </Card>
            </div>
            <h3 className="text-base font-semibold text-black dark:text-white">
              {m.mount_streaming_from_url()}
            </h3>
            <p className="truncate text-sm text-slate-900 dark:text-slate-100">
              {formatters.truncateMiddle(url, 55)}
            </p>
            <p className="text-sm text-slate-900 dark:text-slate-100">
              {formatters.truncateMiddle(filename, 30)}
            </p>
            <p className="text-sm text-slate-900 dark:text-slate-100">
              {formatters.bytes(size ?? 0)}
            </p>
          </div>
        );
      case "Storage":
        return (
          <div className="">
            <div className="mb-0 inline-block">
              <Card>
                <div className="p-1">
                  <LuRadioReceiver className="h-4 w-4 shrink-0 text-blue-700 dark:text-blue-500" />
                </div>
              </Card>
            </div>
            <h3 className="text-base font-semibold text-black dark:text-white">
              {m.mount_mounted_from_storage()}
            </h3>
            <p className="text-sm text-slate-900 dark:text-slate-100">
              {formatters.truncateMiddle(path, 50)}
            </p>
            <p className="text-sm text-slate-900 dark:text-slate-100">
              {formatters.truncateMiddle(filename, 30)}
            </p>
            <p className="text-sm text-slate-900 dark:text-slate-100">
              {formatters.bytes(size ?? 0)}
            </p>
          </div>
        );
      default:
        return null;
    }
  };
  const close = useClose();
  const location = useLocation();

  useEffect(() => {
    syncRemoteVirtualMediaState();
  }, [syncRemoteVirtualMediaState, location.pathname]);

  const { navigateTo } = useDeviceUiNavigation();

  return (
    <GridCard>
      <div className="space-y-4 p-4 py-3">
        <div ref={ref} className="grid h-full grid-rows-(--grid-headerBody)">
          <div className="h-full space-y-4">
            <div className="space-y-4">
              <SettingsPageHeader
                title={m.mount_virtual_media()}
                description={m.mount_virtual_media_description()}
              />

              <div
                className="animate-fadeIn space-y-2 opacity-0"
                style={{
                  animationDuration: "0.7s",
                  animationDelay: "0.1s",
                }}
              >
                <div className="block select-none">
                  <div className="group">
                    <Card>
                      <div className="w-full px-4 py-8">
                        <div className="flex h-full flex-col items-center justify-center text-center">
                          {renderGridCardContent()}
                        </div>
                      </div>
                    </Card>
                  </div>
                </div>
                {remoteVirtualMediaState ? (
                  <div className="flex items-center justify-between text-xs select-none">
                    <div className="text-white select-none dark:text-slate-300">
                      <span>{m.mount_mounted_as()}</span>{" "}
                      <span className="font-semibold">
                        {remoteVirtualMediaState.mode === "Disk"
                          ? m.mount_mode_disk()
                          : m.mount_mode_cdrom()}
                      </span>
                    </div>

                    <div className="flex items-center gap-x-2">
                      <Button
                        size="SM"
                        theme="blank"
                        text={m.close()}
                        onClick={() => {
                          close();
                        }}
                      />
                      <Button
                        size="SM"
                        theme="light"
                        text={m.mount_unmount()}
                        LeadingIcon={({ className }) => (
                          <svg
                            className={`${className} h-2.5 w-2.5 shrink-0`}
                            viewBox="0 0 10 10"
                            fill="none"
                            xmlns="http://www.w3.org/2000/svg"
                          >
                            <g clipPath="url(#clip0_3137_1186)">
                              <path
                                d="M4.99933 0.775635L0 5.77546H10L4.99933 0.775635Z"
                                fill="currentColor"
                              />
                              <path d="M10 7.49976H0V9.22453H10V7.49976Z" fill="currentColor" />
                            </g>
                            <defs>
                              <clipPath id="clip0_3137_1186">
                                <rect width="10" height="10" fill="white" />
                              </clipPath>
                            </defs>
                          </svg>
                        )}
                        onClick={handleUnmount}
                      />
                    </div>
                  </div>
                ) : null}
              </div>
            </div>
          </div>
        </div>

        {!remoteVirtualMediaState && (
          <div
            className="flex animate-fadeIn items-center justify-end space-x-2 opacity-0"
            style={{
              animationDuration: "0.7s",
              animationDelay: "0.2s",
            }}
          >
            <Button
              size="SM"
              theme="blank"
              text={m.close()}
              onClick={() => {
                close();
              }}
            />
            <Button
              size="SM"
              theme="primary"
              text={m.mount_add_new_media()}
              onClick={() => {
                setModalView("mode");
                navigateTo("/mount");
              }}
              LeadingIcon={LuPlus}
            />
          </div>
        )}
      </div>
    </GridCard>
  );
});

MountPopopover.displayName = "MountSidebarRoute";

export default MountPopopover;
