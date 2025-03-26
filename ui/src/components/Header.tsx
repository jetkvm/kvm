import { Fragment, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { ArrowLeftEndOnRectangleIcon, ChevronDownIcon } from "@heroicons/react/16/solid";
import { Menu, MenuButton } from "@headlessui/react";
import { LuMonitorSmartphone } from "react-icons/lu";

import Container from "@/components/Container";
import Card from "@/components/Card";
import { cx } from "@/cva.config";
import { useHidStore, useRTCStore, useUserStore } from "@/hooks/stores";
import LogoBlueIcon from "@/assets/logo-blue.svg";
import LogoWhiteIcon from "@/assets/logo-white.svg";
import USBStateStatus from "@components/USBStateStatus";
import PeerConnectionStatusCard from "@components/PeerConnectionStatusCard";
import { CLOUD_API, DEVICE_API } from "@/ui.config";

import api from "../api";
import { isOnDevice } from "../main";

import { Button, LinkButton } from "./Button";

interface NavbarProps {
  isLoggedIn: boolean;
  primaryLinks?: { title: string; to: string }[];
  userEmail?: string;
  showConnectionStatus?: boolean;
  picture?: string;
  kvmName?: string;
}

export default function DashboardNavbar({
  primaryLinks = [],
  isLoggedIn,
  showConnectionStatus,
  userEmail,
  picture,
  kvmName,
}: NavbarProps) {
  const peerConnectionState = useRTCStore(state => state.peerConnectionState);
  const setUser = useUserStore(state => state.setUser);
  const navigate = useNavigate();
  const onLogout = useCallback(async () => {
    const logoutUrl = isOnDevice ? `${DEVICE_API}/auth/logout` : `${CLOUD_API}/logout`;
    const res = await api.POST(logoutUrl);
    if (!res.ok) return;

    setUser(null);
    // The root route will redirect to appropriate login page, be it the local one or the cloud one
    navigate("/");
  }, [navigate, setUser]);

  const usbState = useHidStore(state => state.usbState);

  return (
    <div className="w-full select-none border-b border-b-slate-800/20 bg-white dark:border-b-slate-300/20 dark:bg-slate-900">
      <Container>
        <div className="flex h-14 items-center justify-between">
          <div className="flex shrink-0 items-center gap-x-8">
            <div className="inline-block shrink-0">
              <img src={LogoBlueIcon} alt="" className="h-[24px] dark:hidden" />
              <img src={LogoWhiteIcon} alt="" className="hidden h-[24px] dark:block" />
            </div>

            <div className="flex gap-x-2">
              {primaryLinks.map(({ title, to }, i) => {
                return (
                  <LinkButton
                    key={i + title}
                    theme="blank"
                    size="SM"
                    text={title}
                    to={to}
                    LeadingIcon={LuMonitorSmartphone}
                  />
                );
              })}
            </div>
          </div>
          <div className="flex w-full items-center justify-end gap-x-2">
            <div className="flex shrink-0 items-center space-x-4">
              {showConnectionStatus && (
                <div className="hidden items-center gap-x-2 md:flex">
                  <div className="w-[159px]">
                    <PeerConnectionStatusCard
                      state={peerConnectionState}
                      title={kvmName}
                    />
                  </div>
                  <div className="hidden w-[159px] md:block">
                    <USBStateStatus
                      state={usbState}
                      peerConnectionState={peerConnectionState}
                    />
                  </div>
                </div>
              )}
              {isLoggedIn ? (
                <>
                  <hr className="h-[20px] w-[1px] border-none bg-slate-800/20 dark:bg-slate-300/20" />
                  <Menu as="div" className="relative inline-block text-left">
                    <div>
                      <MenuButton as={Fragment}>
                        <Button
                          theme="blank"
                          size="SM"
                          text={
                            <>
                              {picture ? <></> : userEmail}
                              <ChevronDownIcon className="h-4 w-4 shrink-0 text-slate-900 dark:text-white" />
                            </>
                          }
                          LeadingIcon={({ className }) =>
                            picture && (
                              <img
                                src={picture}
                                alt="Avatar"
                                className={cx(
                                  className,
                                  "h-8 w-8 rounded-full border-2 border-transparent transition-colors group-hover:border-blue-700",
                                )}
                              />
                            )
                          }
                        />
                      </MenuButton>
                    </div>

                    <Menu.Items className="absolute right-0 z-50 mt-2 w-56 origin-top-right focus:outline-none">
                      <Card className="overflow-hidden">
                        <div className="space-y-1 p-1 dark:text-white">
                          {userEmail && (
                            <div className="border-b border-b-slate-800/20 dark:border-slate-300/20">
                              <Menu.Item>
                                <div className="p-2">
                                  <div className="font-display text-xs">Logged in as</div>
                                  <div className="w-[200px] truncate font-display text-sm font-semibold">
                                    {userEmail}
                                  </div>
                                </div>
                              </Menu.Item>
                            </div>
                          )}
                          <div>
                            <Menu.Item>
                              <div onClick={onLogout}>
                                <button className="block w-full">
                                  <div className="flex items-center gap-x-2 rounded-md px-2 py-1.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700">
                                    <ArrowLeftEndOnRectangleIcon className="h-4 w-4" />
                                    <div className="font-display">Log out</div>
                                  </div>
                                </button>
                              </div>
                            </Menu.Item>
                          </div>
                        </div>
                      </Card>
                    </Menu.Items>
                  </Menu>
                </>
              ) : null}
            </div>
          </div>
        </div>
      </Container>
    </div>
  );
}
