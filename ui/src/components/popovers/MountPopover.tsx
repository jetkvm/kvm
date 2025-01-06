import { Button } from "@components/Button";
import { ExclamationTriangleIcon } from "@heroicons/react/24/outline";
import Card, { GridCard } from "@components/Card";
import { PlusCircleIcon } from "@heroicons/react/20/solid";
import { useMemo, forwardRef, useEffect, useCallback } from "react";
import { formatters } from "@/utils";
import { RemoteVirtualMediaState, useMountMediaStore, useRTCStore } from "@/hooks/stores";
import { SectionHeader } from "@components/SectionHeader";
import {
  LuArrowUpFromLine,
  LuCheckCheck,
  LuLink,
  LuPlus,
  LuRadioReceiver,
} from "react-icons/lu";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "../../notifications";
import MountMediaModal from "../MountMediaDialog";
import { useClose } from "@headlessui/react";

const MountPopopover = forwardRef<HTMLDivElement, object>((_props, ref) => {
  const diskDataChannelStats = useRTCStore(state => state.diskDataChannelStats);
  const [send] = useJsonRpc();
  const {
    remoteVirtualMediaState,
    isMountMediaDialogOpen,
    setModalView,
    setIsMountMediaDialogOpen,
    setRemoteVirtualMediaState,
  } = useMountMediaStore();

  const bytesSentPerSecond = useMemo(() => {
    if (diskDataChannelStats.size < 2) return null;

    const secondLastItem =
      Array.from(diskDataChannelStats)[diskDataChannelStats.size - 2];
    const lastItem = Array.from(diskDataChannelStats)[diskDataChannelStats.size - 1];

    if (!secondLastItem || !lastItem) return 0;

    const lastTime = lastItem[0];
    const secondLastTime = secondLastItem[0];
    const timeDelta = lastTime - secondLastTime;

    const lastBytesSent = lastItem[1].bytesSent;
    const secondLastBytesSent = secondLastItem[1].bytesSent;
    const bytesDelta = lastBytesSent - secondLastBytesSent;

    return bytesDelta / timeDelta;
  }, [diskDataChannelStats]);

  const syncRemoteVirtualMediaState = useCallback(() => {
    send("getVirtualMediaState", {}, response => {
      if ("error" in response) {
        notifications.error(
          `Failed to get virtual media state: ${response.error.message}`,
        );
      } else {
        setRemoteVirtualMediaState(response.result as unknown as RemoteVirtualMediaState);
      }
    });
  }, [send, setRemoteVirtualMediaState]);

  const handleUnmount = () => {
    send("unmountImage", {}, response => {
      if ("error" in response) {
        notifications.error(`Failed to unmount image: ${response.error.message}`);
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
                <PlusCircleIcon className="w-4 h-4 text-blue-700 shrink-0 dark:text-white" />
              </div>
            </Card>
          </div>
          <div className="space-y-1">
            <h3 className="text-sm font-semibold leading-none text-black dark:text-white">
              No mounted media
            </h3>
            <p className="text-xs leading-none text-slate-700 dark:text-slate-300">
              Add a file to get started
            </p>
          </div>
        </div>
      );
    }

    const { source, filename, size, url, path } = remoteVirtualMediaState;

    switch (source) {
      case "WebRTC":
        return (
          <>
            <div className="space-y-1">
              <div className="flex items-center gap-x-2">
                <LuCheckCheck className="h-5 text-green-500" />
                <h3 className="text-base font-semibold text-black dark:text-white">Streaming from Browser</h3>
              </div>
              <Card className="w-auto px-2 py-1">
                <div className="w-full text-sm text-black truncate dark:text-white">
                  {formatters.truncateMiddle(filename, 50)}
                </div>
              </Card>
            </div>
            <div className="flex flex-col items-center my-2 gap-y-2">
              <div className="w-full text-sm text-slate-900 dark:text-slate-100">
                <div className="flex items-center justify-between">
                  <span>{formatters.bytes(size ?? 0)}</span>
                  <div className="flex items-center gap-x-1">
                    <LuArrowUpFromLine className="h-4 text-blue-700 dark:text-blue-500" strokeWidth={2} />
                    <span>
                      {bytesSentPerSecond !== null
                        ? `${formatters.bytes(bytesSentPerSecond)}/s`
                        : "N/A"}
                    </span>
                  </div>
                </div>
              </div>
            </div>
          </>
        );
      case "HTTP":
        return (
          <div className="">
            <div className="inline-block mb-0">
              <Card>
                <div className="p-1">
                  <LuLink className="w-4 h-4 text-blue-700 dark:text-blue-500 shrink-0" />
                </div>
              </Card>
            </div>
            <h3 className="text-base font-semibold text-black dark:text-white">Streaming from URL</h3>
            <p className="text-sm truncate text-slate-900 dark:text-slate-100">{formatters.truncateMiddle(url, 55)}</p>
            <p className="text-sm text-slate-900 dark:text-slate-100">{formatters.truncateMiddle(filename, 30)}</p>
            <p className="text-sm text-slate-900 dark:text-slate-100">{formatters.bytes(size ?? 0)}</p>
          </div>
        );
      case "Storage":
        return (
          <div className="">
            <div className="inline-block mb-0">
              <Card>
                <div className="p-1">
                  <LuRadioReceiver className="w-4 h-4 text-blue-700 dark:text-blue-500 shrink-0" />
                </div>
              </Card>
            </div>
            <h3 className="text-base font-semibold text-black dark:text-white">Mounted from JetKVM Storage</h3>
            <p className="text-sm text-slate-900 dark:text-slate-100">{formatters.truncateMiddle(path, 50)}</p>
            <p className="text-sm text-slate-900 dark:text-slate-100">{formatters.truncateMiddle(filename, 30)}</p>
            <p className="text-sm text-slate-900 dark:text-slate-100">{formatters.bytes(size ?? 0)}</p>
          </div>
        );
      default:
        return null;
    }
  };
  const close = useClose();

  useEffect(() => {
    syncRemoteVirtualMediaState();
  }, [syncRemoteVirtualMediaState, isMountMediaDialogOpen]);

  return (
    <GridCard>
      <div className="p-4 py-3 space-y-4">
        <div ref={ref} className="grid h-full grid-rows-headerBody">
          <div className="h-full space-y-4 ">
            <div className="space-y-4">
              <SectionHeader
                title="Virtual Media"
                description="Mount an image to boot from or install an operating system."
              />

              {remoteVirtualMediaState?.source === "WebRTC" ? (
                <Card>
                  <div className="flex items-center gap-x-1.5 px-2.5 py-2 text-sm">
                    <ExclamationTriangleIcon className="h-4 text-yellow-500" />
                    <div className="flex items-center w-full text-black">
                      <div>Closing this tab will unmount the image</div>
                    </div>
                  </div>
                </Card>
              ) : null}

              <div
                className="space-y-2 opacity-0 animate-fadeIn"
                style={{
                  animationDuration: "0.7s",
                  animationDelay: "0.1s",
                }}
              >
                <div className="block select-none">
                  <div className="group">
                    <Card>
                      <div className="w-full px-4 py-8">
                        <div className="flex flex-col items-center justify-center h-full text-center">
                          {renderGridCardContent()}
                        </div>
                      </div>
                    </Card>
                  </div>
                </div>
                {remoteVirtualMediaState ? (
                  <div className="flex items-center justify-between text-xs select-none">
                    <div className="text-white select-none dark:text-slate-300">
                      <span>Mounted as</span>{" "}
                      <span className="font-semibold">
                        {remoteVirtualMediaState.mode === "Disk" ? "Disk" : "CD-ROM"}
                      </span>
                    </div>

                    <div className="flex items-center gap-x-2">
                      <Button
                        size="SM"
                        theme="blank"
                        text="Close"
                        onClick={() => {
                          close();
                        }}
                      />
                      <Button
                        size="SM"
                        theme="light"
                        text="Unmount"
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

          <MountMediaModal
            open={isMountMediaDialogOpen}
            setOpen={setIsMountMediaDialogOpen}
          />
        </div>

        {!remoteVirtualMediaState && (
          <div
            className="flex items-center justify-end space-x-2 opacity-0 animate-fadeIn"
            style={{
              animationDuration: "0.7s",
              animationDelay: "0.2s",
            }}
          >
            <Button
              size="SM"
              theme="blank"
              text="Close"
              onClick={() => {
                close();
              }}
            />
            <Button
              size="SM"
              theme="primary"
              text="Add New Media"
              onClick={() => {
                setModalView("mode");
                setIsMountMediaDialogOpen(true);
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
